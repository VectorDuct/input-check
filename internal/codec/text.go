// Package codec performs deterministic, reversible transformations of explicit input spans.
package codec

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxTextBytes = 8 << 20
const maxOutputBytes = 32 << 20
const maxTags = 8192
const maxDepth = 64

// Recognize complete input names, never arbitrary prose containing "输入".
const inputName = `(?:(?:本轮|本回合|本次|当前|原始|最新)?(?:用户输入|玩家输入|user输入|player输入|user_input|player_input)|(?:本轮|本回合|本次|当前|最新)输入|历史前文|剧情原文|now_plot|latest_input|additional_information|past_observe|content)`

var tags = regexp.MustCompile(`(?i)<(/?)(` + inputName + `)>`)
var inputHeading = regexp.MustCompile(`(?i)^` + inputName + `$`)
var headings = regexp.MustCompile(`(?m)^[\t ]*【([^【】\r\n]+)】[\t ]*\r?$`)
var ErrLimit = errors.New("text transformation limit exceeded")
var ErrUTF8 = errors.New("text must be valid UTF-8")

type Transformer struct {
	chunk          int
	tagPattern     *regexp.Regexp
	headingPattern *regexp.Regexp
}
type Stats struct{ Matched, Encoded, Skipped, Ambiguous, Partial int }
type frame struct {
	kind           string
	start, content int
	invalid        bool
}
type span struct{ start, end int }

func New(chunkRunes int) (*Transformer, error) {
	if chunkRunes < 1 || chunkRunes > 256 {
		return nil, errors.New("chunk_runes must be between 1 and 256")
	}
	return &Transformer{chunk: chunkRunes, tagPattern: tags, headingPattern: inputHeading}, nil
}

func (t *Transformer) Text(s string) (string, Stats, error) {
	return t.TextLimit(s, maxOutputBytes)
}

// TextLimit bounds output before allocating expanded Base64 envelopes and
// reports structural ambiguity so partially encoded blocks remain visible.
func (t *Transformer) TextLimit(s string, limit int) (string, Stats, error) {
	out, stats, err := t.textLimitCore(s, limit)
	if err != nil {
		return out, stats, err
	}
	matches := t.tagPattern.FindAllStringSubmatchIndex(s, maxTags+1)
	opens, closes := 0, 0
	for _, m := range matches {
		if m[3] == m[2] {
			opens++
		} else {
			closes++
		}
	}
	if opens != closes {
		stats.Ambiguous = 1
		if stats.Encoded > 0 {
			stats.Partial = 1
		}
	}
	return out, stats, nil
}

// textLimitCore is the original bounded transformation. Pairing is unchanged.
func (t *Transformer) textLimitCore(s string, limit int) (string, Stats, error) {
	var stats Stats
	if limit < 0 {
		return "", stats, ErrLimit
	}
	if limit > maxOutputBytes {
		limit = maxOutputBytes
	}
	if len(s) > maxTextBytes {
		return "", stats, ErrLimit
	}
	if !utf8.ValidString(s) {
		return "", stats, ErrUTF8
	}
	matches := t.tagPattern.FindAllStringSubmatchIndex(s, maxTags+1)
	if len(matches) > maxTags {
		return "", stats, ErrLimit
	}
	var stack []frame
	var candidates []span
	var structuralBounds []span
	for _, m := range matches {
		kind := strings.ToLower(s[m[4]:m[5]])
		if m[3] == m[2] {
			if len(stack) >= maxDepth {
				return "", stats, ErrLimit
			}
			stack = append(stack, frame{kind: kind, start: m[0], content: m[1]})
			continue
		}
		at := len(stack) - 1
		for at >= 0 && !strings.EqualFold(stack[at].kind, kind) {
			at--
		}
		if at < 0 {
			continue
		}
		if at != len(stack)-1 {
			// Crossing tag families invalidate the involved openers, not unrelated spans.
			for k := at; k < len(stack); k++ {
				stack[k].invalid = true
			}
		}
		f := stack[at]
		stack = append(stack[:at], stack[at+1:]...)
		structuralBounds = append(structuralBounds, span{f.start, m[1]})
		if !f.invalid {
			candidates = append(candidates, span{f.content, m[0]})
		}
	}
	boundaries := headings.FindAllStringSubmatchIndex(s, maxTags+1)
	if len(matches)+len(boundaries) > maxTags {
		return "", stats, ErrLimit
	}
	protected := mergeBounds(structuralBounds)
	// Paired angle regions (including crossing/invalid pairs) are structural
	// units. Their internal headings cannot delimit an outside paragraph.
	// Otherwise encoding can hide a boundary or repair a crossing on pass two.
	visible := boundaries[:0]
	for _, h := range boundaries {
		at := sort.Search(len(protected), func(j int) bool { return protected[j].end > h[0] })
		if at < len(protected) && protected[at].start < h[1] {
			continue
		}
		visible = append(visible, h)
	}
	boundaries = visible
	for i, h := range boundaries {
		if !t.headingPattern.MatchString(strings.TrimSpace(s[h[2]:h[3]])) {
			continue
		}
		begin := h[1]
		if begin < len(s) && s[begin] == '\n' {
			begin++
		}
		end := len(s)
		if i+1 < len(boundaries) {
			end = boundaries[i+1][0]
			// Keep the next title on its own line, including on the second pass.
			// The separating CRLF/LF belongs to the section boundary, not the envelope.
			if end > begin && s[end-1] == '\n' {
				end--
				if end > begin && s[end-1] == '\r' {
					end--
				}
			}
		}
		p := span{begin, end}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		if len(s) > limit {
			return "", stats, ErrLimit
		}
		return s, stats, nil
	}
	selected := outermost(candidates)
	var out strings.Builder
	cursor := 0
	for _, p := range selected {
		stats.Matched++
		if p.start-cursor > limit-out.Len() {
			return "", Stats{}, ErrLimit
		}
		out.WriteString(s[cursor:p.start])
		content, encoded, skipped, err := t.payload(s[p.start:p.end], limit-out.Len()-(len(s)-p.end))
		if err != nil {
			return "", Stats{}, err
		}
		out.WriteString(content)
		stats.Encoded += encoded
		stats.Skipped += skipped
		cursor = p.end
		if out.Len() > limit {
			return "", Stats{}, ErrLimit
		}
	}
	if len(s)-cursor > limit-out.Len() {
		return "", Stats{}, ErrLimit
	}
	out.WriteString(s[cursor:])
	if out.Len() > limit {
		return "", Stats{}, ErrLimit
	}
	return out.String(), stats, nil
}

// Union structural bounds so crossing pairs remain one protected region.
func mergeBounds(bounds []span) []span {
	sort.Slice(bounds, func(i, j int) bool { return bounds[i].start < bounds[j].start })
	var merged []span
	for _, p := range bounds {
		if len(merged) > 0 && p.start <= merged[len(merged)-1].end {
			if p.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = p.end
			}
		} else {
			merged = append(merged, p)
		}
	}
	return merged
}

// Pick nonoverlapping outer ranges so nested rules are encoded once.
func outermost(candidates []span) []span {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].start == candidates[j].start {
			return candidates[i].end > candidates[j].end
		}
		return candidates[i].start < candidates[j].start
	})
	var selected []span
	for _, p := range candidates {
		if len(selected) > 0 && p.start < selected[len(selected)-1].end {
			continue
		}
		selected = append(selected, p)
	}
	return selected
}

func (t *Transformer) payload(s string, limit int) (string, int, int, error) {
	if limit < 0 {
		return "", 0, 0, ErrLimit
	}
	var out strings.Builder
	cursor := 0
	search := 0
	closeAt := -1
	encoded, skipped := 0, 0
	for search < len(s) {
		offset := strings.Index(s[search:], envelopeStart)
		if offset < 0 {
			break
		}
		at := search + offset
		if closeAt < at {
			nextClose := strings.Index(s[at:], envelopeEnd)
			if nextClose < 0 {
				break // No remaining opening can form a complete envelope.
			}
			closeAt = at + nextClose
		}
		// A canonical block cannot contain another opening marker: chunks are Base64.
		// Cache the close position so repeated malformed starts are scanned linearly.
		nextOpen := strings.Index(s[at+len(envelopeStart):], envelopeStart)
		if nextOpen >= 0 && at+len(envelopeStart)+nextOpen < closeAt {
			search = at + len(envelopeStart) + nextOpen
			continue
		}
		end, ok := validEnvelope(s[at : closeAt+len(envelopeEnd)])
		if !ok {
			search = at + len(envelopeStart)
			continue
		}
		if at > cursor {
			value, err := t.encode(s[cursor:at], limit-out.Len()-(end))
			if err != nil {
				return "", 0, 0, err
			}
			out.WriteString(value)
			encoded++
		}
		if end > limit-out.Len() {
			return "", 0, 0, ErrLimit
		}
		out.WriteString(s[at : at+end])
		skipped++
		cursor = at + end
		search = cursor
		if out.Len() > limit {
			return "", 0, 0, ErrLimit
		}
	}
	if cursor < len(s) {
		value, err := t.encode(s[cursor:], limit-out.Len())
		if err != nil {
			return "", 0, 0, err
		}
		out.WriteString(value)
		encoded++
	}
	if out.Len() > limit {
		return "", 0, 0, ErrLimit
	}
	return out.String(), encoded, skipped, nil
}

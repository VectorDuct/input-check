// Package check reports structural input diagnostics without echoing content.
package check

import (
	"encoding/json"
	"inputcheck/internal/codec"
	"regexp"
	"strings"
	"unicode/utf8"
)

var DefaultTags = []string{"input", "user_input", "context", "content", "用户输入"}
var headings = regexp.MustCompile(`(?m)^[\t ]*【([^【】\r\n]+)】[\t ]*\r?$`)

type Finding struct {
	Code         string `json:"code"`
	Severity     string `json:"severity"`
	MessageIndex int    `json:"message_index"`
	BlockIndex   int    `json:"block_index"`
	Offset       int    `json:"offset"`
}
type Report struct {
	Valid          bool      `json:"valid"`
	InputBytes     int       `json:"input_bytes"`
	Messages       int       `json:"messages"`
	TextBlocks     int       `json:"text_blocks"`
	PairedTags     int       `json:"paired_tags"`
	HeadingMarkers int       `json:"heading_markers"`
	Warnings       int       `json:"warnings"`
	Findings       []Finding `json:"findings"`
	Truncated      bool      `json:"truncated"`
}
type Checker struct{ tags, heading *regexp.Regexp }

func New(names []string) (*Checker, error) {
	if names == nil {
		names = DefaultTags
	}
	tags, heading, err := codec.CompileTags(names)
	if err != nil {
		return nil, err
	}
	return &Checker{tags, heading}, nil
}
func (r *Report) add(code, severity string, message, block, offset int) {
	if severity == "error" {
		r.Valid = false
	} else {
		r.Warnings++
	}
	if len(r.Findings) < 128 {
		r.Findings = append(r.Findings, Finding{code, severity, message, block, offset})
	} else {
		r.Truncated = true
	}
}
func (r *Report) fail(code string) { r.add(code, "error", -1, -1, -1) }

func (c *Checker) Inspect(raw []byte) Report {
	r := Report{Valid: true, InputBytes: len(raw), Findings: []Finding{}}
	if len(raw) > 8<<20 {
		r.fail("input_too_large")
		return r
	}
	if !utf8.Valid(raw) {
		r.fail("invalid_utf8")
		return r
	}
	if err := UniqueJSON(raw); err != nil {
		r.fail(err.Error())
		return r
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		r.fail("invalid_schema")
		return r
	}
	var model string
	if json.Unmarshal(root["model"], &model) != nil || model == "" || len(model) > 256 {
		r.fail("invalid_model")
		return r
	}
	var messages []json.RawMessage
	if json.Unmarshal(root["messages"], &messages) != nil || len(messages) < 1 || len(messages) > 1024 {
		r.fail("invalid_messages")
		return r
	}
	r.Messages = len(messages)
	totalBlocks := 0
	for i, message := range messages {
		var m map[string]json.RawMessage
		if json.Unmarshal(message, &m) != nil || m == nil {
			r.fail("invalid_message")
			return r
		}
		var role string
		if json.Unmarshal(m["role"], &role) != nil {
			r.fail("invalid_role")
			return r
		}
		inspect := true
		switch role {
		case "tool", "function":
			inspect = false
		case "system", "developer", "user", "assistant":
		default:
			r.fail("invalid_role")
			return r
		}
		content := m["content"]
		if len(content) == 0 || string(content) == "null" {
			if role == "system" || role == "developer" || role == "user" {
				r.fail("missing_content")
				return r
			}
			continue
		}
		var text string
		if len(content) > 0 && content[0] == '"' && json.Unmarshal(content, &text) == nil {
			if inspect {
				r.TextBlocks++
				c.inspectText(text, i, -1, &r)
			}
			continue
		}
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(content, &blocks) != nil || len(blocks) == 0 {
			r.fail("invalid_content")
			return r
		}
		totalBlocks += len(blocks)
		if totalBlocks > 2048 {
			r.fail("content_block_limit")
			return r
		}
		for j, block := range blocks {
			var kind string
			if block == nil || json.Unmarshal(block["type"], &kind) != nil || kind == "" {
				r.fail("invalid_content_block")
				return r
			}
			if kind != "text" {
				continue
			}
			field := block["text"]
			if len(field) == 0 || field[0] != '"' || json.Unmarshal(field, &text) != nil {
				r.fail("invalid_text_block")
				return r
			}
			if inspect {
				r.TextBlocks++
				c.inspectText(text, i, j, &r)
			}
		}
	}
	return r
}

type frame struct {
	name   string
	offset int
}

func (c *Checker) inspectText(text string, message, block int, r *Report) {
	matches := c.tags.FindAllStringSubmatchIndex(text, 8193)
	titles := headings.FindAllStringSubmatchIndex(text, 8193)
	if len(matches)+len(titles) > 8192 {
		r.add("structure_limit", "error", message, block, -1)
		return
	}
	stack := []frame{}
	for _, m := range matches {
		name := strings.ToLower(text[m[4]:m[5]])
		if m[2] == m[3] {
			if len(stack) >= 64 {
				r.add("structure_limit", "error", message, block, m[0])
				return
			}
			stack = append(stack, frame{name, m[0]})
			continue
		}
		at := len(stack) - 1
		for at >= 0 && !strings.EqualFold(stack[at].name, name) {
			at--
		}
		if at < 0 {
			r.add("unmatched_close", "warning", message, block, m[0])
			continue
		}
		if at != len(stack)-1 {
			r.add("crossed_tags", "warning", message, block, m[0])
		} else {
			r.PairedTags++
		}
		stack = append(stack[:at], stack[at+1:]...)
	}
	for _, f := range stack {
		r.add("unclosed_tag", "warning", message, block, f.offset)
	}
	for _, h := range titles {
		if c.heading.MatchString(strings.TrimSpace(text[h[2]:h[3]])) {
			r.HeadingMarkers++
		}
	}
	for offset, ch := range text {
		if ch < 32 && ch != '\n' && ch != '\r' && ch != '\t' || ch == 127 {
			r.add("control_character", "warning", message, block, offset)
		}
		if ch >= 0x202a && ch <= 0x202e || ch >= 0x2066 && ch <= 0x2069 {
			r.add("bidi_control", "warning", message, block, offset)
		}
	}
	const start = "[[OBF-B64/1 "
	const end = "[[/OBF-B64/1]]"
	cursor, closeAt, count := 0, -1, 0
	for cursor < len(text) {
		relative := strings.Index(text[cursor:], start)
		if relative < 0 {
			break
		}
		at := cursor + relative
		count++
		if count > 8192 {
			r.add("structure_limit", "error", message, block, at)
			return
		}
		if closeAt < at {
			p := strings.Index(text[at:], end)
			if p < 0 {
				r.add("invalid_envelope", "warning", message, block, at)
				break
			}
			closeAt = at + p
		}
		next := strings.Index(text[at+len(start):], start)
		if next >= 0 && at+len(start)+next < closeAt {
			r.add("invalid_envelope", "warning", message, block, at)
			cursor = at + len(start) + next
			continue
		}
		size, ok := codec.ValidateEnvelope(text[at:])
		if !ok {
			r.add("invalid_envelope", "warning", message, block, at)
			cursor = closeAt + len(end)
		} else {
			cursor = at + size
		}
	}
}

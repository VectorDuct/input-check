package codec

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// CompileTags accepts literal marker names, never operator-supplied regexes.
func CompileTags(names []string) (*regexp.Regexp, *regexp.Regexp, error) {
	if len(names) < 1 || len(names) > 32 {
		return nil, nil, errors.New("configure 1..32 tag names")
	}
	escaped := make([]string, 0, len(names))
	seen := []string{}
	for _, name := range names {
		if len(name) == 0 || len(name) > 128 {
			return nil, nil, errors.New("invalid or duplicate tag name")
		}
		for _, old := range seen {
			if strings.EqualFold(old, name) {
				return nil, nil, errors.New("invalid or duplicate tag name")
			}
		}
		for _, r := range name {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
				return nil, nil, errors.New("tag names allow letters, digits, underscores and hyphens")
			}
		}
		seen = append(seen, name)
		escaped = append(escaped, regexp.QuoteMeta(name))
	}
	pattern := strings.Join(escaped, "|")
	return regexp.MustCompile(`(?i)<(/?)(` + pattern + `)>`), regexp.MustCompile(`(?i)^(?:` + pattern + `)$`), nil
}

func NewWithTags(chunkRunes int, names []string) (*Transformer, error) {
	tr, err := New(chunkRunes)
	if err != nil {
		return nil, err
	}
	tr.tagPattern, tr.headingPattern, err = CompileTags(names)
	if err != nil {
		return nil, err
	}
	return tr, nil
}

// ValidateEnvelope checks this tool's canonical Base64/UTF-8/length/checksum format.
// The checksum detects corruption; it is neither authentication nor encryption.
func ValidateEnvelope(text string) (int, bool) { return validEnvelope(text) }

package gateway

import (
	"encoding/json"
	"inputcheck/internal/codec"
)

// rawJSONSize predicts encoding/json's compact, HTML-escaped RawMessage size.
// Callers first validate the JSON. No expanded copy is needed to count bytes.
func rawJSONSize(raw []byte) int {
	n := 0
	quoted := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !quoted && (c == ' ' || c == '\n' || c == '\r' || c == '\t') {
			continue
		}
		if c == '"' {
			quoted = !quoted
			n++
			continue
		}
		if quoted && c == '\\' {
			n += 2
			i++
			continue
		}
		if quoted && (c == '<' || c == '>' || c == '&') {
			n += 6
			continue
		}
		if quoted && c == 0xe2 && i+2 < len(raw) && raw[i+1] == 0x80 && (raw[i+2] == 0xa8 || raw[i+2] == 0xa9) {
			n += 6
			i += 2
			continue
		}
		n++
	}
	return n
}

func jsonStringSize(s string) int {
	n := 2
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' || c == '\b' || c == '\f':
			n += 2
		case c < 0x20 || c == '<' || c == '>' || c == '&':
			n += 6
		case c == 0xe2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xa8 || s[i+2] == 0xa9):
			n += 6
			i += 2
		default:
			n++
		}
	}
	return n
}

// Only the concrete shapes edited by this gateway are supported.
func marshalBounded(value any, limit int) ([]byte, error) {
	size := 0
	switch v := value.(type) {
	case map[string]json.RawMessage:
		size = 2
		for k, raw := range v {
			size += jsonStringSize(k) + 1 + rawJSONSize(raw) + 1
		}
		if len(v) > 0 {
			size--
		}
	case []json.RawMessage:
		size = 2
		for _, raw := range v {
			size += rawJSONSize(raw) + 1
		}
		if len(v) > 0 {
			size--
		}
	case string:
		size = jsonStringSize(v)
	default:
		return nil, errRequest
	}
	if size > limit {
		return nil, codec.ErrLimit
	}
	return json.Marshal(value)
}

type outputBudget struct {
	remaining int
	limit     int
}

func (b *outputBudget) text(tr *codec.Transformer, text string) (string, codec.Stats, error) {
	if b.remaining < 0 {
		// Identity requests can retain their original JSON without HTML escaping.
		// Prove this text is unchanged using a budget that cannot permit expansion.
		value, stats, err := tr.TextLimit(text, len(text))
		if err != nil {
			return "", stats, err
		}
		if value != text {
			return "", stats, codec.ErrLimit
		}
		return value, stats, nil
	}
	before := jsonStringSize(text)
	// JSON string encoding needs at least two quotes plus len(value) bytes.
	value, stats, err := tr.TextLimit(text, b.remaining+before-2)
	if err != nil {
		return "", stats, err
	}
	growth := jsonStringSize(value) - before
	if growth > b.remaining {
		return "", stats, codec.ErrLimit
	}
	if growth > 0 {
		b.remaining -= growth
	}
	return value, stats, nil
}

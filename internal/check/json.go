package check

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
)

// UniqueJSON rejects duplicate keys at every level before any map decoding.
func UniqueJSON(raw []byte) error {
	if !validUnicodeEscapes(raw) {
		return errors.New("invalid_unicode_escape")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("json_depth_limit")
		}
		token, err := dec.Token()
		if err != nil {
			return errors.New("invalid_json")
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return errors.New("invalid_json")
				}
				name, ok := key.(string)
				if !ok {
					return errors.New("invalid_json")
				}
				if seen[name] {
					return errors.New("duplicate_json_key")
				}
				seen[name] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("invalid_json")
			}
		case json.Delim('['):
			for dec.More() {
				if err = value(depth + 1); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("invalid_json")
			}
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("invalid_json")
	}
	return nil
}

// encoding/json replaces lone surrogates. Reject them instead of silently
// changing text when a checked document is later reserialized for encoding.
func validUnicodeEscapes(raw []byte) bool {
	quoted := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || raw[i] != '\\' {
			continue
		}
		if i+1 >= len(raw) {
			return true
		}
		if raw[i+1] != 'u' {
			i++
			continue
		}
		if i+6 > len(raw) {
			return true
		}
		n, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
		if err != nil {
			return true
		}
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+12 > len(raw) || raw[i+6] != '\\' || raw[i+7] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 11
		} else {
			i += 5
		}
	}
	return true
}

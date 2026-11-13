package codec

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const envelopeStart = "[[OBF-B64/1 "
const envelopeEnd = "[[/OBF-B64/1]]"
const decodeHint = "Decode each numbered Base64 chunk as UTF-8; concatenate in order, preserving all whitespace."

func (t *Transformer) encode(s string, limit int) (string, error) {
	// Calculate exact size without allocating the expanded representation.
	size := len(envelopeStart) + len("bytes=") + len(strconv.Itoa(len(s))) + len(" sha256=") + 64 + len("]]\n") + len(decodeHint) + 1 + len(envelopeEnd)
	if size > limit {
		return "", ErrLimit
	}
	start, count, sequence := 0, 0, 1
	add := func(n int) bool {
		size += len(strconv.Itoa(sequence)) + 1 + base64.StdEncoding.EncodedLen(n) + 1
		sequence++
		return size <= limit
	}
	for at := range s {
		if count == t.chunk {
			if !add(at - start) {
				return "", ErrLimit
			}
			start, count = at, 0
		}
		count++
	}
	if start < len(s) && !add(len(s)-start) {
		return "", ErrLimit
	}

	var out strings.Builder
	out.Grow(size)
	digest := sha256.Sum256([]byte(s))
	fmt.Fprintf(&out, "%sbytes=%d sha256=%x]]\n%s\n", envelopeStart, len(s), digest, decodeHint)
	start, count, sequence = 0, 0, 1
	for at := range s {
		if count == t.chunk {
			fmt.Fprintf(&out, "%d:%s\n", sequence, base64.StdEncoding.EncodeToString([]byte(s[start:at])))
			sequence++
			start = at
			count = 0
		}
		count++
	}
	if start < len(s) {
		fmt.Fprintf(&out, "%d:%s\n", sequence, base64.StdEncoding.EncodeToString([]byte(s[start:])))
	}
	out.WriteString(envelopeEnd)
	return out.String(), nil
}

// validEnvelope validates only this gateway's complete, canonical, checksummed format.
// No arbitrary Base64-looking user text is trusted or skipped.
func validEnvelope(s string) (int, bool) {
	if !strings.HasPrefix(s, envelopeStart) {
		return 0, false
	}
	e := strings.Index(s, envelopeEnd)
	if e < 0 {
		return 0, false
	}
	block := s[:e]
	lines := strings.Split(block, "\n")
	if len(lines) < 4 || lines[1] != decodeHint || lines[len(lines)-1] != "" {
		return 0, false
	}
	header := strings.TrimPrefix(lines[0], envelopeStart)
	fields := strings.Fields(strings.TrimSuffix(header, "]]"))
	if len(fields) != 2 || !strings.HasSuffix(header, "]]") || !strings.HasPrefix(fields[0], "bytes=") || !strings.HasPrefix(fields[1], "sha256=") {
		return 0, false
	}
	size, err := strconv.Atoi(strings.TrimPrefix(fields[0], "bytes="))
	if err != nil || size <= 0 || size > maxTextBytes {
		return 0, false
	}
	expected := strings.TrimPrefix(fields[1], "sha256=")
	hash, err := hex.DecodeString(expected)
	if err != nil || len(hash) != 32 {
		return 0, false
	}
	digest := sha256.New()
	total := 0
	for i, line := range lines[2 : len(lines)-1] {
		number, chunk, ok := strings.Cut(line, ":")
		if !ok || number != strconv.Itoa(i+1) || chunk == "" {
			return 0, false
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(chunk)
		if err != nil || !utf8.Valid(raw) || len(raw) == 0 || utf8.RuneCount(raw) > 256 || base64.StdEncoding.EncodeToString(raw) != chunk {
			return 0, false
		}
		total += len(raw)
		if total > size {
			return 0, false
		}
		digest.Write(raw)
	}
	if total != size || hex.EncodeToString(digest.Sum(nil)) != expected {
		return 0, false
	}
	// Require the exact header, not alternate spellings/leading zeros/extra spacing.
	if lines[0] != fmt.Sprintf("%sbytes=%d sha256=%s]]", envelopeStart, size, expected) {
		return 0, false
	}
	return e + len(envelopeEnd), true
}

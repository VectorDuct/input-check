# Optional text-envelope encoding

Encoding is disabled by default. It is available through `inputcheck encode` or a proxy explicitly configured with `mode: encode`. A valid, warning-free inspection is required first. Plain `check` and `/v1/check` never return a transformed prompt.

The codec transforms complete configured `<name>...</name>` contents and standalone full-line `【name】` sections. Full-line headings end at the next standalone `【any title】` or message end. Angle-tag regions protect their internal headings from changing an outside section boundary. Nested selected ranges encode once at the outermost range. Pairing stays within one message/text block. Tags, surrounding text, whitespace and line breaks outside selected contents remain as represented in the decoded text. Tool/function results, tool definitions and arguments, images and non-text blocks remain opaque.

Inside an eligible nonempty region, the synthetic text `hello` is represented as:

```text
[[OBF-B64/1 bytes=5 sha256=2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824]]
Decode each numbered Base64 chunk as UTF-8; concatenate in order, preserving all whitespace.
1:aGVsbG8=
[[/OBF-B64/1]]
```

The `OBF-B64/1` identifier is retained from the extracted codec for wire compatibility. Each chunk represents at most `chunk_runes` codepoints, independently UTF-8/Base64 encoded. Chunk sequence, canonical syntax, strict Base64, valid UTF-8, byte length and SHA-256 must all match to recognize an existing envelope. The decode instruction is part of this format.

This representation is reversible and visible to anyone who receives it. SHA-256 detects accidental corruption; anyone can recompute it. It provides no encryption, authenticity, secrecy, redaction or semantic safety. The tool does not market encoding as protection from moderation, prompt injection or authorization policy. A model may misunderstand it, ignore the instruction, lose context or consume more tokens. Application behavior must be evaluated independently.

Validated existing envelopes remain unchanged; arbitrary Base64-looking strings are not trusted or decoded. Mixed plain text and valid envelopes can be processed without double encoding. Resource limits still apply to already-encoded requests. No-match requests preserve their text and can still be subject to configured model alias mapping. Empty selected regions are matched but not expanded.

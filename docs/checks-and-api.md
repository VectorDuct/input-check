# Checks and HTTP API

## Supported document

The root must be a JSON object with a nonempty string `model` (at most 256 bytes) and `messages` containing 1–1,024 objects. Roles are `system`, `developer`, `user`, `assistant`, `tool`, or `function`.

`system`/`developer`/`user` require non-null content. `assistant`/`tool`/`function` may omit content or use null. Non-null content must be a string or nonempty array of objects with a string `type`. A `type: "text"` block requires a string `text`. At most 2,048 array content blocks per document. Other block shapes are opaque and not validated against provider-specific schemas. Top-level tool definitions, arguments, tool/function results and non-text data are not text-inspected or transformed. Tool result content still passes the common content-shape check.

Raw input must be UTF-8. JSON object keys must be unique at every depth, including escaped equivalents. JSON nesting is bounded to 64; unpaired UTF-16 surrogate escapes are rejected instead of being silently replaced by Go's decoder. Unknown fields and large numeric literals are retained when forwarding/encoding; they are not converted through floating point.

## Text rules

Names are literal Unicode letters/digits/underscores/hyphens, at most 128 bytes, 1–32 unique case-folded names. Matching is case-insensitive using Unicode simple folding. Exact `<name>` and `</name>` markers only: attributes, extra whitespace, arbitrary XML names or natural-language mentions are not parsed.

Default names: `input`, `user_input`, `context`, `content`, `用户输入`. Complete matching nested pairs are counted. Crossed/unmatched/unclosed configured markers cause warnings. Unconfigured markers are ignored. Standalone full-line `【name】` headings are counted as literal marker occurrences, independently of tag pairs; this count is not an estimate of encoded regions.

Per text block: at most 8,192 configured tag tokens plus standalone heading lines, and at most 64 open configured tags. Supported numbered Base64 envelopes are checked for canonical syntax, sequence, Base64, UTF-8, byte length and SHA-256; damaged markers cause a warning. C0 characters other than tab/CR/LF, DEL, and Unicode bidi controls U+202A–U+202E/U+2066–U+2069 cause warnings. These characters can have legitimate uses: a warning is not an abuse verdict.

## Reports

```json
{
  "valid": true,
  "input_bytes": 97,
  "messages": 1,
  "text_blocks": 1,
  "paired_tags": 0,
  "heading_markers": 0,
  "warnings": 1,
  "findings": [{"code":"unclosed_tag","severity":"warning","message_index":0,"block_index":-1,"offset":0}],
  "truncated": false
}
```

Counters above are illustrative. `valid` means no structural/schema/resource **errors**; it does not mean warning-free, trusted, safe or accepted by every configured policy. No risk score is invented. Indexes are zero-based. `block_index: -1` denotes string content, and root-level errors use -1 for all positions. Offsets refer to UTF-8 bytes in decoded strings, not raw JSON bytes. At most 128 findings are returned; the total warning count continues and `truncated` indicates omitted findings. Fixed codes avoid copying user text into reports.

Warning codes: `unclosed_tag`, `unmatched_close`, `crossed_tags`, `control_character`, `bidi_control`, `invalid_envelope`.

Error codes include `invalid_utf8`, `invalid_unicode_escape`, `invalid_json`, `duplicate_json_key`, `json_depth_limit`, `invalid_schema`, `invalid_model`, `invalid_messages`, `invalid_message`, `invalid_role`, `missing_content`, `invalid_content`, `invalid_content_block`, `invalid_text_block`, `content_block_limit`, `structure_limit`, `input_too_large`.

## Routes

- `POST /v1/check`: authenticated, never forwards. 200 for structurally valid input (warnings may be present); 400 for structural errors; 422 if `reject_warnings` rejects a warning. Encoding mode does not transform this endpoint's input.
- `POST /v1/chat/completions`: exists only with a configured upstream and model map. 400/422 checks happen before forwarding. `mode: encode` always rejects warnings. Otherwise valid requests stream the upstream status/body; no automatic retries. An unknown model alias is rejected before upstream dispatch, even if a standalone shape check would pass.
- `GET /healthz`: local startup/readiness only. No authentication required and no upstream/model probe. It does not prove provider health.

Other paths are 404, unsupported methods 405, missing/invalid Bearer auth 401, wrong content type 415, configured request/output expansion limits 413, full admission 503, fixed-upstream failure 502 and upstream deadline 504. Body-read failures are 400/408 depending on stage/error. All POST routes require `application/json`; queries and compressed bodies are rejected. Inspection reports and local error envelopes use `Cache-Control: no-store`.

Forwarded responses carry `X-InputCheck-Mode`, warning count and transform metadata. Transform status is `no_match`, `matched`, `encoded` or `already_encoded`; in inspect mode it remains `no_match` because no transformation runs. Encoded/ambiguous/partial counters describe the codec only, not semantic safety. The check API report supplies actual structural findings. Upstream copies of these headers and Set-Cookie are stripped. Intermediaries may remove custom headers, so do not treat them as guaranteed end-user UI delivery.

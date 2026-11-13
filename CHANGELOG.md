# Changelog

## 0.1.0 — initial release

- Standalone structural inspector for Chat Completions JSON, with metadata-only diagnostics.
- Checks UTF-8, Unicode escapes, duplicate JSON keys, depth/resource bounds, message/content shape, configured tag boundaries, unusual control/bidirectional characters and canonical envelope integrity.
- Local check/explicit-encode CLI, authenticated `/v1/check`, optional fixed-upstream gateway.
- Inspection is the default and preserves request bytes with identity model mappings; reversible Base64 transformation is explicit and refuses warnings.
- Configurable literal markers; separate client/upstream credentials; bounded concurrency and a shared request deadline; graceful shutdown.
- MIT license, dual-language documentation, synthetic fixtures, Docker/Compose, CI and allowlisted source archive.

# Changelog

## 0.1.0 — initial release candidate

- Standalone structural inspector for Chat Completions JSON, with metadata-only diagnostics.
- Checks UTF-8, Unicode escapes, duplicate JSON keys, depth/resource bounds, message/content shape, configured tag boundaries, unusual control/bidirectional characters and canonical envelope integrity.
- Local check/explicit-encode CLI, authenticated `/v1/check`, optional fixed-upstream gateway.
- Inspection is the default and preserves request bytes with identity model mappings; reversible Base64 transformation is explicit and refuses warnings.
- Configurable literal markers; separate client/upstream credentials; bounded concurrency and a shared request deadline; graceful shutdown.
- MIT license, dual-language documentation, synthetic fixtures, Docker/Compose, CI and allowlisted source archive.

This candidate has no production migration, model-generation test, semantic safety certification or hosted CI result. Checks diagnose structural issues; encoding is neither encryption nor a safety filter.

Local validation: 61 top-level Go test items (66 including subtests), `go vet` and a static Linux build passed using Go 1.25 in a resource-limited offline container. A real-process synthetic upstream check verified auth, shape rejection, strict gating, unchanged inspection forwarding, SSE, header/credential separation, graceful in-flight drain and explicit encoding. The COPY-only scratch image ran check CLI and local health readiness as UID/GID 65532 with network disabled and a read-only root filesystem. The multi-stage Dockerfile and hosted CI are provided for dedicated build machines; no hosted run or vulnerability scan is claimed.

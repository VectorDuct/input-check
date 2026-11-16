# Changelog

Notable changes are documented using [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) categories. Release dates reflect publication of the corresponding version.

## [Unreleased]

## [0.1.0] - 2026-11-16

### Added

- Private vulnerability disclosure policy and expanded contributor verification guide.
- EditorConfig settings for UTF-8, LF line endings, final newlines, and Go/Makefile tab indentation.
- Synthetic multi-turn function-calling request in `examples/request.tool-call.json` for strict CLI inspection.
- Standalone structural inspector for Chat Completions JSON, with metadata-only diagnostics.
- Checks UTF-8, Unicode escapes, duplicate JSON keys, depth/resource bounds, message/content shape, configured tag boundaries, unusual control/bidirectional characters and canonical envelope integrity.
- Local check/explicit-encode CLI, authenticated `/v1/check`, optional fixed-upstream gateway.
- Inspection is the default and preserves request bytes with identity model mappings; reversible Base64 transformation is explicit and refuses warnings.
- Configurable literal markers; separate client/upstream credentials; bounded concurrency and a shared request deadline; graceful shutdown.
- MIT license, dual-language documentation, synthetic fixtures, Docker/Compose, CI and allowlisted source archive.
- Wire-Level Guard runs on Go 1.25+ with no third-party runtime dependencies; the source archive helper uses Python.

- Deterministic source release ZIP including security policy, contributor guide, EditorConfig, examples, and a per-file SHA-256 manifest.

### Fixed

- Source archive manifests use portable forward-slash paths and platform-independent filename ordering.

### Security

- Inspection does not rewrite tool arguments, tool results, or opaque non-text blocks and does not guarantee semantic prompt safety.
- Diagnostics omit request-body snippets and credentials; fixed-upstream gateway operation uses separate client/upstream credentials.
- No external telemetry beacon; operators remain responsible for deployment access and upstream configuration.

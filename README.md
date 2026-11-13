# Input Check (`input-check`)

**[简体中文](./README.zh-CN.md)** · **[Checks & API](./docs/checks-and-api.md)** · **[Configuration](./docs/configuration.md)** · **[Envelope Format](./docs/encoding.md)**

A structural payload inspector and deterministic fixed-upstream gateway for Chat Completions applications. Validate model requests locally via CLI or over HTTP, collect metadata-only diagnostics without echoing prompt content, and optionally enforce structural checks in front of a fixed upstream endpoint.

## Key Features

- **Wire-Level JSON & UTF-8 Validation**: Enforces strict UTF-8 encoding, Unicode escape validity, duplicate JSON key rejection, and configurable nesting depth and byte-size limits.
- **Chat Completions Shape Verification**: Validates message role sequences and text block structures before upstream dispatch.
- **Configurable Delimiter & Marker Checks**: Detects unclosed or crossed tags, standalone heading markers, unexpected control/bidirectional Unicode characters, and corrupted transport envelopes.
- **Zero-Content Diagnostics**: Emits fixed diagnostic codes, counts, and zero-based byte offsets—never logging or returning prompt snippets in reports.
- **CLI, HTTP Check API & Fixed-Upstream Gateway**: Run standalone local checks, expose an authenticated `/v1/check` endpoint, or proxy validated `/v1/chat/completions` traffic to a configured upstream with model allowlisting and SSE streaming.
- **Standard Library Go**: Built exclusively with the Go standard library (`Go 1.25+`) with bounded concurrency, strict request deadlines, and graceful shutdown.

## Quick Start

Compile the static binary and inspect sample payloads locally:

```bash
CGO_ENABLED=0 GOMAXPROCS=1 go build -p 1 -trimpath -o bin/inputcheck ./cmd/inputcheck
./bin/inputcheck check -input examples/request.json
./bin/inputcheck check -strict -input examples/warnings.json
```

Under `-strict`, structural warnings return exit code `1` alongside a metadata-only diagnostic report:

```json
{
  "valid": true,
  "warnings": 1,
  "findings": [
    {
      "code": "unclosed_tag",
      "severity": "warning",
      "message_index": 0,
      "block_index": -1,
      "offset": 0
    }
  ]
}
```

| Exit Code | Meaning |
| :--- | :--- |
| `0` | Valid document (warnings permitted unless `-strict` is set). |
| `1` | Valid structure with warnings rejected by `-strict` policy. |
| `2` | Malformed input, invalid configuration, resource ceiling exceeded, or runtime error. |

## HTTP Inspection API

Set a client bearer token (at least 16 bytes) in `INPUTCHECK_TOKEN` and start the inspection server:

```bash
./bin/inputcheck serve -config examples/config.inspect.json -check-config
./bin/inputcheck serve -config examples/config.inspect.json
```

Send a payload to `/v1/check`:

```bash
curl --fail-with-body \
  -H "Authorization: Bearer $INPUTCHECK_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @examples/request.json \
  http://127.0.0.1:9090/v1/check
```

## Fixed-Upstream Gateway

To run `input-check` as an inline validation gateway in front of an upstream provider, copy `examples/config.proxy.json` to `config.json` and configure your upstream origin, model allowlist mapping, and two distinct tokens (`INPUTCHECK_TOKEN` for downstream clients and `INPUTCHECK_UPSTREAM_TOKEN` for the upstream provider):

```bash
./bin/inputcheck serve -config config.json -check-config
./bin/inputcheck serve -config config.json
```

Send standard Chat Completions requests to `/v1/chat/completions`. When `reject_warnings: true` is enabled, requests triggering structural warnings are rejected locally before any upstream call is made.

## Deterministic Transport Envelope (`encode`)

For multi-hop middleware pipelines requiring binary-safe text transport with checksum verification:

```bash
./bin/inputcheck encode -input examples/request.json
```

When invoked via CLI or `mode: "encode"`, validated text segments are packaged into numbered Base64 chunks with SHA-256 integrity hashes, leaving tool calls, arguments, and non-text fields untouched.

## Docker Deployment

After setting `INPUTCHECK_TOKEN`:

```bash
docker compose up --build -d
```

The container runs as `UID/GID 65532` on `127.0.0.1:9090` with a read-only root filesystem, dropped Linux capabilities, and explicit memory/CPU limits.

## Privacy & Resource Bounds

- **Zero Payload Persistence**: Request bodies are validated in memory and never written to disk. Proxy logs record request counts and latency timings only.
- **Default Ceilings**: `1 MiB` inspection input, `8 MiB` forwarded proxy payload, `2` concurrent requests, and a `60s` total request deadline (all configurable within documented bounds).

## Development & Verification

```bash
make test
make vet
make build
python3 tools/runtime_smoke.py --binary bin/inputcheck
```

## License

MIT License. Maintained by [VectorDuct](https://vectorduct.com).

# Input Check

A structural input inspector for model applications. Check Chat Completions requests locally or over HTTP, get diagnostics without echoing the prompt, and optionally place the checks in front of a fixed upstream.

[简体中文](README.zh-CN.md) · [Checks and API](docs/checks-and-api.md) · [Configuration](docs/configuration.md) · [Encoding format](docs/encoding.md)

**Inspection is the default.** Reversible input encoding is a separate, explicit feature. A successful structural check is not a content-safety verdict.

## Features

- UTF-8 and Unicode-escape validation, duplicate JSON key rejection and depth/size limits.
- Message roles and text-block shape checks for Chat Completions requests.
- Configurable literal markers: unclosed/crossed tags, standalone heading markers, unusual control/bidirectional characters and damaged encoding envelopes.
- Metadata-only diagnostics: fixed codes, counts and byte positions; no prompt snippets in check reports or logs.
- Local CLI, authenticated check API, optional fixed-upstream proxy with separate credentials, model allowlist and streaming responses.
- Bounded concurrency, request deadlines, graceful shutdown; Go standard library only.

## Quick start

Requires Go 1.25+. Build from this checkout:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 go build -p 1 -trimpath -o bin/inputcheck ./cmd/inputcheck
./bin/inputcheck check -input examples/request.json
./bin/inputcheck check -strict -input examples/warnings.json
```

The second command returns exit code 1 and a diagnostic such as:

```json
{
  "valid": true,
  "warnings": 1,
  "findings": [{"code": "unclosed_tag", "severity": "warning", "message_index": 0, "block_index": -1, "offset": 0}]
}
```

This abbreviated example omits report counters. `valid` means the document passes structural/schema bounds; warnings still need attention. No network request is made by `check` or `encode`.

| Exit code | Meaning |
| --- | --- |
| `0` | Valid document; warnings permitted unless strict mode is selected |
| `1` | Valid structure with warnings rejected by strict policy; encode always rejects warnings |
| `2` | Invalid input/configuration, resource limit or execution failure |

Omit `-input` or use `-input -` to read stdin. Select marker names with `-tags input,customer_note,资料`; no regular expressions are accepted. Reports never echo text; offsets are zero-based byte positions in the decoded text block.

## Check API

Configure a random client token of at least 16 bytes in the `INPUTCHECK_TOKEN` environment variable using your secret manager or local environment. Then:

```sh
./bin/inputcheck serve -config examples/config.inspect.json -check-config
./bin/inputcheck serve -config examples/config.inspect.json
```

```sh
curl --fail-with-body \
  -H "Authorization: Bearer $INPUTCHECK_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @examples/request.json \
  http://127.0.0.1:9090/v1/check
```

The included inspector configuration has **no upstream**. It only validates the document and returns a report. Configuration checks read no credentials and make no requests; they do not prove credentials or connectivity work.

## Optional gateway

Copy `examples/config.proxy.json` to a private `config.json`. Set your fixed upstream origin, model mapping and two distinct token sources. `INPUTCHECK_TOKEN` authenticates clients to this service; `INPUTCHECK_UPSTREAM_TOKEN` authenticates this service to your upstream.

```sh
./bin/inputcheck serve -config config.json -check-config
./bin/inputcheck serve -config config.json
```

Send normal Chat Completions requests to `/v1/chat/completions`. In default `inspect` mode, valid requests pass checks and retain their exact bytes when model aliases map to themselves. An alias mapping changes the model field and serializes the document; decoded text and unknown JSON fields remain intact. The upstream origin cannot come from a request. Other paths, queries, compressed request bodies and redirects are rejected.

`reject_warnings: true` blocks warned requests before any upstream call. The standalone `/v1/check` endpoint never forwards, even if an upstream is configured. It checks document structure, not whether the model is available or permitted by that upstream; the proxy separately enforces the configured model map.

## Explicit reversible encoding

For workflows that deliberately require the documented text-envelope representation:

```sh
./bin/inputcheck encode -input examples/request.json
```

Or set `mode: "encode"` in your private proxy configuration. Encoding runs only after a valid check with no warnings. It transforms selected text regions and leaves tool results, tool arguments, images and other non-text fields untouched. No-match requests are unchanged apart from any requested model mapping. A validated existing envelope is not encoded again.

The format uses numbered Base64 chunks and SHA-256 for corruption detection. It is **not encryption, redaction, moderation or protection against prompt injection**. It expands text and changes the model input representation; downstream interpretation and compatibility must be tested for your application. See the exact [format and limitations](docs/encoding.md). The encode CLI intentionally emits request content; route that output only where you intend.

## Docker

After setting `INPUTCHECK_TOKEN`:

```sh
docker compose up --build -d
```

The bundled Compose runs the check-only API on `127.0.0.1:9090`, as UID/GID 65532, with a read-only filesystem, no Linux capabilities and explicit resource limits. It mounts only the synthetic inspector config and has no provider connection configured. To use a proxy, mount your own config read-only and supply the separate upstream credential. Secret files are supported as alternatives to environment variables.

The multi-stage Dockerfile builds on a dedicated development/CI machine. A shared machine can compile first under its resource policy and then use the COPY-only `Dockerfile.prebuilt`; its context must contain only the reviewed binary, CA bundle and LICENSE. Update the pinned builder digest deliberately when maintaining the project.

## Privacy and limits

No request body is persisted. Check responses contain diagnostic metadata only. The optional proxy deliberately forwards accepted requests to the configured upstream and streams its response; there are no automatic retries or fallback origins. Upstream response bodies are not inspected. Proxy logs contain counts/timings, never prompt snippets or credentials. There is no telemetry or background model probing.

Default bounds: 1 MiB input, 8 MiB forwarded request, 2 concurrent requests and a 60-second total deadline; marker/JSON nesting and report counts are bounded too. See [configuration](docs/configuration.md) for supported ranges. Use HTTPS or a trusted local network for token-bearing traffic. The supplied loopback binding can sit behind your existing authenticated HTTPS proxy. Keep private configs, tokens and any downstream logging policies outside the repository.

This is a structural checker for the documented JSON dialect, not a full provider schema validator, XML parser, semantic classifier, malicious-intent detector or guarantee that a prompt is safe. Unknown non-text content is opaque. It does not operate accounts or login queues, set billing policy or bypass upstream authorization.

## Development and release

```sh
make test
make vet
make build
python3 tools/runtime_smoke.py --binary bin/inputcheck
python3 tools/release.py
```

Tests use synthetic strings and local fake HTTP upstreams. They do not require credentials, call models or reuse production prompts. The release helper creates an allowlisted, reproducible source ZIP with a per-file SHA-256 manifest under `dist/`. Review its file list and content before publishing; private deployment files and build caches are excluded.

MIT. See [provenance](PROVENANCE.md) and [release notes](CHANGELOG.md). No public repository, package registry, live provider validation or hosted CI result is implied by this candidate.

# Configuration

`serve -config path.json` loads a private JSON object; unknown and duplicate keys are rejected. With no config the service is check-only, listens on 127.0.0.1:9090 and reads `INPUTCHECK_TOKEN`. `-check-config` validates structure using synthetic credential placeholders, without reading credentials, connecting or starting a listener. Runtime credential checks happen at startup. Changes require restart.

| Field | Default / range |
| --- | --- |
| `listen` | `127.0.0.1:9090`; literal IP or localhost with port 1–65535 |
| `mode` | `inspect`; only `inspect` or explicit `encode` |
| `tags` | `input`, `user_input`, `context`, `content`, `用户输入`; 1–32 literal names |
| `reject_warnings` | false; inspect mode may allow warnings; encode always refuses them |
| `upstream` | absent means check-only; HTTP(S) origin, no path except `/`, credentials, query or fragment |
| `models` | required with upstream: 1–32 alias-to-upstream-model mappings; not needed for check-only |
| `gateway_token_env` / `gateway_token_file` | choose one; default environment name `INPUTCHECK_TOKEN` |
| `upstream_token_env` / `upstream_token_file` | choose one; default `INPUTCHECK_UPSTREAM_TOKEN` if upstream configured |
| `chunk_runes` | 24; 1–256 Unicode codepoints per encoded chunk |
| `max_input_bytes` | 1,048,576; 1–8,388,608 |
| `max_output_bytes` | 8,388,608; 1–33,554,432; applies to the forwarded/transformed request, not upstream response bodies |
| `max_concurrent` | 2; 1–16 across both POST endpoints; no unlimited setting |
| `timeout_seconds` | 60; 1–3,600; total active request deadline, plus at most one second for a stalled response writer/error grace |
| `shutdown_seconds` | 90; 1–7,200; initial graceful wait, then continue waiting without force-closing active requests |

Config is at most 64 KiB. CLI check/encode can reuse tag/size settings without loading credentials. Explicit null/zero fields may be invalid; omit a setting to retain its documented default. `max_input_bytes` is enforced before parsing. A forwarded identity request must still fit `max_output_bytes`. The inherited encoding budget is conservative for JSON escaping and may reject inputs whose normalized form might fit.

Environment fields are variable **names**, not token values. Secret files must be absolute paths and readable by runtime UID 65532 for Docker, or the account running the CLI. A token contains 16–4,096 bytes, no embedded ASCII whitespace; surrounding whitespace/newline is trimmed. Client and upstream tokens must differ. Protect files and config at the filesystem level; use mode 0400 with appropriate ownership for secret files and restricted parent directories. Never mount a whole provider account directory.

`models` is an allowlist, not automatic discovery. The gateway never creates model permissions or changes upstream entitlement. Identity mappings preserve exact request bytes in inspect mode. Alias mapping serializes the request, preserving decoded text and unknown JSON fields, then calls only `/v1/chat/completions` on the fixed origin. Response model names and body data remain what the upstream returned; they are not rewritten back to aliases.

Traffic authentication is independent at each hop. The proxy sends its own upstream token and selected Content-Type/User-Agent headers; client Cookies, forwarding headers and credentials are not copied. Ambient HTTP proxy environment variables are ignored. Use HTTPS outside a deliberately trusted network. Redirects are not followed, and upstream 3xx responses are converted to an error instead of exposing a relocation target to the caller.

A graceful supervisor stop timeout should exceed the request deadline plus response grace and operational margin; the bundled Compose uses 95 seconds for the 60-second default. This protects active requests inside the process, but cannot override an external supervisor that sends SIGKILL. No operational scripts to replace production services are included.

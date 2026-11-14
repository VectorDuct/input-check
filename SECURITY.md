# Security Policy

## Supported Versions

| Version | Security fixes |
| --- | --- |
| 0.1.x | Supported; use the latest available patch |
| Other versions | Not supported |

## Reporting a Vulnerability

Report suspected vulnerabilities privately to [contact@vectorduct.com](mailto:contact@vectorduct.com) or [founders@vectorduct.com](mailto:founders@vectorduct.com). Do not publish vulnerability details or exploit instructions in a public GitHub issue before coordinated disclosure.

We aim to acknowledge reports within 48 hours; this is not a guaranteed repair deadline. Include the affected version, configuration with secrets removed, expected and observed behavior, and a minimal synthetic request. Do not send live gateway tokens, upstream keys, private prompts, or full environment dumps. Coordinate public disclosure with the maintainers after assessment and remediation.

## Security Boundary

- The checker validates request structure and configured transport/text boundaries. It does not establish semantic prompt safety, tool authorization, or model-output safety.
- Inspection is the default. Tool arguments, tool results, and opaque non-text blocks must not be rewritten; input transformation remains explicit.
- Gateway operation uses a configured fixed upstream, separate client and upstream credentials, and bounded request resources. Operators must secure the host, configuration, environment, and network exposure.
- Diagnostics contain fixed codes, counts, durations, and positions rather than request-body snippets or credentials. Do not place secrets in issue reports or fixtures.
- There is no external telemetry beacon. Upstream forwarding is an explicitly configured gateway function, not telemetry. The service is not an upstream credential vault.

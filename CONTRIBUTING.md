# Contributing

Run `make test`, `make vet` and `make build` before proposing changes. Keep tests synthetic and exercise real loopback HTTP servers for proxy/auth/stream/deadline changes. Add regressions for changed schema, marker, Unicode or envelope behavior.

Keep runtime code standard-library only. New rules must describe exactly what they inspect and cannot be advertised as semantic safety guarantees. Tool arguments, tool results and opaque non-text blocks must not be rewritten. Inspection is the default; input modification must remain explicit.

Do not submit real request bodies, account records, tokens, operational snapshots or full environment dumps in issues or patches. Reports and logs must contain only fixed diagnostic codes, counts, durations and positions. Use private security reporting if the hosting repository enables it for vulnerabilities.

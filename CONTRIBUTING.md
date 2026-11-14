# Contributing

## Local Verification

Use Go 1.25 or newer, Python 3.11 or newer for the source release helper, and a POSIX shell with Make. From the repository root, run:

```bash
make test
make vet
make build
./bin/inputcheck check -strict -input examples/request.json
./bin/inputcheck serve -config examples/config.inspect.json -check-config
python3 tools/release.py
```

CI also verifies the container build with Docker:

```bash
docker build -t inputcheck:test .
```

The Makefile uses POSIX environment assignments; on Windows, use an appropriate POSIX environment rather than pasting those assignments into PowerShell. Keep tests synthetic and exercise real loopback HTTP servers for proxy/auth/stream/deadline changes. Add regressions for changed schema, marker, Unicode or envelope behavior.

## Code Style and Boundaries

Format Go changes with `gofmt`. Keep source and fixtures valid UTF-8; malformed encoding belongs only in explicit negative-test data. Keep changes focused and include verification results in pull requests.

Keep runtime code standard-library only. New rules must describe exactly what they inspect and cannot be advertised as semantic safety guarantees. Tool arguments, tool results and opaque non-text blocks must not be rewritten. Inspection is the default; input modification must remain explicit.

Do not submit real request bodies, account records, tokens, operational snapshots or full environment dumps in issues or patches. Reports and logs must contain only fixed diagnostic codes, counts, durations and positions. Report suspected vulnerabilities privately as described in [SECURITY.md](./SECURITY.md), not in public issues. Do not add external telemetry.

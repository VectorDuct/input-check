FROM golang:1.25-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS builder
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOTOOLCHAIN=local GOMAXPROCS=1 go build -p 1 -trimpath -ldflags="-s -w" -o /inputcheck ./cmd/inputcheck

FROM scratch AS runtime
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chown=65532:65532 /inputcheck /inputcheck
COPY LICENSE /LICENSE
USER 65532:65532
ENV GOMEMLIMIT=192MiB GOMAXPROCS=1
EXPOSE 9090
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/inputcheck", "healthcheck", "-config", "/etc/inputcheck/config.json"]
ENTRYPOINT ["/inputcheck"]
CMD ["serve", "-config", "/etc/inputcheck/config.json"]

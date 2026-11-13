.PHONY: test vet build release

test:
	GOTOOLCHAIN=local GOMAXPROCS=1 go test -p 1 ./... -count=1 -timeout 60s

vet:
	GOTOOLCHAIN=local GOMAXPROCS=1 go vet -p 1 ./...

build:
	CGO_ENABLED=0 GOTOOLCHAIN=local GOMAXPROCS=1 go build -p 1 -trimpath -o bin/inputcheck ./cmd/inputcheck

release:
	python3 tools/release.py

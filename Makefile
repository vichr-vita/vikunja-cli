CLI_BIN ?= bin/vikunja-cli

.PHONY: build test install smoke

build:
	mkdir -p "$(dir $(CLI_BIN))"
	go build -o "$(CLI_BIN)" ./cmd/vikunja-cli

test:
	go test ./...
	go vet ./...

install:
	go install ./cmd/vikunja-cli

smoke: build
	./scripts/cli-smoke.sh "$(CLI_BIN)"

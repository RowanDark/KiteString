BINARY    := ks
MODULE    := github.com/RowanDark/kitestring
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE      ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -ldflags "\
  -X $(MODULE)/internal/cli.Version=$(VERSION) \
  -X $(MODULE)/internal/cli.Commit=$(COMMIT) \
  -X $(MODULE)/internal/cli.BuildDate=$(DATE) \
  -X $(MODULE)/internal/cli.BuiltBy=make"

.PHONY: build test lint vet proto snapshot clean install wordlists

## build: compile ks binary for the current platform into bin/
build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/ks

## test: run full test suite with race detector
test:
	go test ./... -race -coverprofile=coverage.out -covermode=atomic

## vet: run go vet
vet:
	go vet ./...

## lint: run golangci-lint
lint:
	golangci-lint run --config .golangci.yml

## proto: regenerate protobuf bindings
proto:
	protoc --go_out=. --go_opt=paths=source_relative pkg/ksfile/ksfile.proto

## snapshot: build release archives for all platforms (no tag required)
snapshot:
	goreleaser release --snapshot --clean

## clean: remove build artifacts
clean:
	rm -rf bin/ dist/ coverage.out

## install: install ks into GOPATH/bin
install:
	go install $(LDFLAGS) ./cmd/ks

## wordlists: fetch and compile all wordlists locally using the ks binary
wordlists: build
	./bin/$(BINARY) wordlist seclists fetch --all
	@echo "Wordlists fetched and compiled into ~/.cache/kitestring/wordlists/"

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'

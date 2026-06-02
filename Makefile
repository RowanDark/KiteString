BINARY   := ks
MODULE   := github.com/RowanDark/kitestring
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X $(MODULE)/internal/cli.Version=$(VERSION)"

.PHONY: build vet test clean install proto

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/ks

vet:
	go vet ./...

test:
	go test ./...

clean:
	rm -rf bin/

install:
	go install $(LDFLAGS) ./cmd/ks

proto:
	protoc --go_out=. --go_opt=paths=source_relative pkg/ksfile/ksfile.proto

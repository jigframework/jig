.PHONY: build test lint fmt tidy

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X github.com/jigframework/jig.Version=$(VERSION)" -o bin/jig ./cmd/jig

test:
	go test -race ./...

lint:
	go vet ./...
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping"

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

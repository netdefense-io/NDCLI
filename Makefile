BINARY_NAME=ndcli
MCP_BINARY_NAME=netdefense-mcp
TUI_BINARY_NAME=netdefense
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS=-ldflags "-s -w -X github.com/netdefense-io/NDCLI/internal/config.Version=$(VERSION) -X github.com/netdefense-io/NDCLI/internal/config.BuildTime=$(BUILD_TIME)"

.PHONY: all build build-mcp build-tui build-all test lint clean install

all: build build-mcp build-tui

build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/ndcli

build-mcp:
	go build $(LDFLAGS) -o bin/$(MCP_BINARY_NAME) ./cmd/netdefense-mcp

build-tui:
	go build $(LDFLAGS) -o bin/$(TUI_BINARY_NAME) ./cmd/netdefense

build-all: build-darwin build-linux build-windows

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/ndcli
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/ndcli
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(MCP_BINARY_NAME)-darwin-amd64 ./cmd/netdefense-mcp
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(MCP_BINARY_NAME)-darwin-arm64 ./cmd/netdefense-mcp
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(TUI_BINARY_NAME)-darwin-amd64 ./cmd/netdefense
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(TUI_BINARY_NAME)-darwin-arm64 ./cmd/netdefense

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/ndcli
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/ndcli
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(MCP_BINARY_NAME)-linux-amd64 ./cmd/netdefense-mcp
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(MCP_BINARY_NAME)-linux-arm64 ./cmd/netdefense-mcp
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(TUI_BINARY_NAME)-linux-amd64 ./cmd/netdefense
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(TUI_BINARY_NAME)-linux-arm64 ./cmd/netdefense

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/ndcli
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(MCP_BINARY_NAME)-windows-amd64.exe ./cmd/netdefense-mcp
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(TUI_BINARY_NAME)-windows-amd64.exe ./cmd/netdefense

test:
	go test -v -race -cover ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/

install: build build-mcp build-tui
	cp bin/$(BINARY_NAME) $(GOPATH)/bin/
	cp bin/$(MCP_BINARY_NAME) $(GOPATH)/bin/
	cp bin/$(TUI_BINARY_NAME) $(GOPATH)/bin/

deps:
	go mod download
	go mod tidy

run:
	go run ./cmd/ndcli $(ARGS)

run-mcp:
	go run ./cmd/netdefense-mcp

run-tui:
	go run ./cmd/netdefense $(ARGS)

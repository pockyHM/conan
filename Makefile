.PHONY: build build-linux build-darwin clean

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/conan ./cmd/conan
	go build -ldflags "$(LDFLAGS)" -o bin/conan-agent ./cmd/conan-agent

build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/conan-linux-amd64 ./cmd/conan
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/conan-agent-linux-amd64 ./cmd/conan-agent
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-linux-arm64 ./cmd/conan
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-agent-linux-arm64 ./cmd/conan-agent

build-darwin:
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/conan-darwin-amd64 ./cmd/conan
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/conan-agent-darwin-amd64 ./cmd/conan-agent
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-darwin-arm64 ./cmd/conan
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/conan-agent-darwin-arm64 ./cmd/conan-agent

clean:
	rm -rf bin/

test:
	go test ./...

test-verbose:
	go test -v ./...

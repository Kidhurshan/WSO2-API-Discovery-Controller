# WSO2 API Discovery Controller — Build System
MODULE     = github.com/wso2/adc
BINARY     = adc
VERSION   ?= $(shell cat VERSION 2>/dev/null || echo "dev")
BUILD_TIME = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS    = -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)

# ── Build ──

.PHONY: build
build:  ## Build binary for current platform
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/adc/

.PHONY: build-linux
build-linux:  ## Build for Linux amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 ./cmd/adc/

.PHONY: build-linux-arm
build-linux-arm:  ## Build for Linux arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/adc/

.PHONY: build-all
build-all: build-linux build-linux-arm  ## Build for all target platforms

# ── Test ──

.PHONY: test
test:  ## Run unit tests
	go test -v -race -cover ./internal/...

.PHONY: test-integration
test-integration:  ## Run integration tests (requires Docker for testcontainers)
	go test -v -tags=integration -timeout 120s ./test/integration/...

.PHONY: test-all
test-all: test test-integration  ## Run all tests

# ── Lint ──

.PHONY: lint
lint:  ## Run golangci-lint
	golangci-lint run ./...

.PHONY: fmt
fmt:  ## Format code
	gofmt -w -s .

# ── Docker ──

.PHONY: docker
docker:  ## Build Docker image
	docker build -t wso2/adc:$(VERSION) -f deploy/docker/Dockerfile .

# ── Development ──

.PHONY: run
run:  ## Run locally with example config
	go run ./cmd/adc/ --config config/config.example.toml

.PHONY: validate
validate:  ## Validate example config
	go run ./cmd/adc/ --config config/config.example.toml --validate

# ── Schema ──

.PHONY: schema
schema:  ## Concatenate migrations into complete DDL
	cat schema/migrations/*.sql > schema/adc_schema.sql

# ── Clean ──

.PHONY: clean
clean:  ## Remove build artifacts
	rm -rf bin/

.PHONY: help
help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

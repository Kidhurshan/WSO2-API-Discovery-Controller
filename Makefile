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

.PHONY: docker-up
docker-up:  ## Start bundled docker-compose stack (postgres + adc)
	cd deploy/docker && [ -f .env ] || cp .env.example .env
	cd deploy/docker && docker compose up -d

.PHONY: docker-up-external
docker-up-external:  ## Start docker-compose against external postgres
	cd deploy/docker && [ -f .env.external-db ] || cp .env.external-db.example .env.external-db
	cd deploy/docker && docker compose -f docker-compose.external-db.yml up -d

.PHONY: docker-down
docker-down:  ## Stop bundled docker-compose stack
	cd deploy/docker && docker compose down

.PHONY: docker-down-external
docker-down-external:  ## Stop external-db docker-compose stack
	cd deploy/docker && docker compose -f docker-compose.external-db.yml down

# ── systemd (VM) ──

.PHONY: install
install: build-linux  ## Install ADC as a systemd service (bundled postgres). Use INSTALL_FLAGS="--external-db --yes" to override.
	sudo deploy/systemd/install.sh $(INSTALL_FLAGS)

.PHONY: uninstall
uninstall:  ## Uninstall ADC systemd service. Use UNINSTALL_FLAGS="--purge --drop-db" to wipe data.
	sudo deploy/systemd/uninstall.sh $(UNINSTALL_FLAGS)

# ── Kubernetes ──

.PHONY: k8s-apply
k8s-apply:  ## Apply all K8s manifests via kustomize (bundled postgres)
	kubectl apply -k deploy/kubernetes/

.PHONY: k8s-delete
k8s-delete:  ## Delete all K8s manifests via kustomize
	kubectl delete -k deploy/kubernetes/

# ── Development ──

.PHONY: run
run:  ## Run locally with config template
	go run ./cmd/adc/ --config config/config.toml

.PHONY: validate
validate:  ## Validate config template
	go run ./cmd/adc/ --config config/config.toml --validate

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

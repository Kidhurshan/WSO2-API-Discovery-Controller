# WSO2 API Discovery Controller (ADC)

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-13+-336791?logo=postgresql&logoColor=white)](https://www.postgresql.org/)

A Go daemon that discovers unmanaged APIs via [DeepFlow](https://deepflow.io/) eBPF traffic capture, compares them against APIs managed in [WSO2 API Manager](https://wso2.com/api-manager/), generates OpenAPI 3.0.3 specifications, and pushes them to the APIM Service Catalog for governance.

## Architecture

```
  DeepFlow eBPF                WSO2 API Manager
  (Traffic Capture)            (Publisher API)
        │                            │
        ▼                            ▼
  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────────┐
  │ Phase 1  │    │ Phase 2  │    │ Phase 3  │    │ Phase 4  │    │   Phase 5    │
  │Discovery │───▶│ Managed  │───▶│Comparison│───▶│ Spec Gen │───▶│Service Catalog│
  │          │    │  Sync    │    │          │    │          │    │    Push       │
  └──────────┘    └──────────┘    └──────────┘    └──────────┘    └──────────────┘
        │               │              │               │                │
        └───────────────┴──────────────┴───────────────┴────────────────┘
                                       │
                                  PostgreSQL
                               (State Store)
```

## Features

- **eBPF-based discovery** — Captures real HTTP traffic via DeepFlow without sidecars or agents
- **Managed API sync** — Fetches APIs from WSO2 APIM Publisher to establish a baseline
- **SHADOW + DRIFT classification** — Identifies completely unknown APIs (SHADOW) and managed APIs with undocumented operations (DRIFT)
- **OpenAPI 3.0.3 generation** — Auto-generates specs from observed traffic patterns with enriched metadata
- **Service Catalog push** — Makes discovered APIs visible in WSO2 APIM for governance
- **Catalog reconciliation** — Detects and re-pushes deleted or modified catalog entries
- **Path normalization** — Collapses dynamic segments (UUIDs, numeric IDs) into `{id}` placeholders
- **Noise filtering** — Excludes health checks, K8s internals, cloud metadata, and static files
- **Circuit breakers** — Per-phase circuit breakers with exponential backoff for external services
- **Autonomous operation** — DB startup retry, HTTP retry with jitter, non-fatal DeepFlow init
- **Health probes** — `/healthz` (liveness) and `/readyz` (readiness with DB check)
- **Data retention** — Automatic cleanup of stale discovered, managed, and unmanaged records

## Prerequisites

| Component | Version | Purpose |
|-----------|---------|---------|
| Go | 1.22+ | Build from source |
| PostgreSQL | 13+ | State store (tables auto-created) |
| DeepFlow | 7.0+ | eBPF traffic capture |
| WSO2 API Manager | 4.x | Managed API source + Service Catalog target |

## Quick Start

```bash
# Build
make build

# Configure
cp config/config.toml /etc/adc/config.toml
# Edit /etc/adc/config.toml — fill in PostgreSQL, DeepFlow, and APIM details

# Validate config
./bin/adc --config /etc/adc/config.toml --validate

# Run
./bin/adc --config /etc/adc/config.toml
```

## Configuration

ADC uses a single TOML configuration file. See [`config/config.toml`](config/config.toml) for a fully documented template with all available options.

| Section | Purpose |
|---------|---------|
| `[server]` | Logging, health port, data retention |
| `[discovery]` | Phase 1 — DeepFlow connection, traffic/noise filters, normalization |
| `[managed]` | Phase 2 — WSO2 APIM connection, auth, sync schedule |
| `[comparison]` | Phase 3 — SHADOW/DRIFT detection toggle |
| `[spec_generation]` | Phase 4 — OpenAPI generation options |
| `[service_catalog]` | Phase 5 — Catalog push behavior, reconciliation |
| `[catalog.datastore]` | PostgreSQL connection (**always required**) |

Phases can be enabled independently. Minimum config: just `[catalog.datastore]` with all phases disabled.

## Deployment

### VM / Bare Metal (systemd)

```bash
# Build for Linux
make build-linux

# Copy binary and config
sudo cp bin/adc-linux-amd64 /usr/local/bin/adc
sudo cp config/config.toml /etc/adc/config.toml

# Install systemd service
sudo cp deploy/systemd/adc.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now adc
```

### Docker

```bash
make docker
docker run -v /etc/adc/config.toml:/etc/adc/config.toml wso2/adc:0.1.0
```

### Kubernetes

```bash
kubectl apply -f deploy/kubernetes/namespace.yaml
kubectl create secret generic adc-config -n adc-system --from-file=config.toml=/etc/adc/config.toml
kubectl apply -f deploy/kubernetes/
```

Kubernetes manifests include: Deployment (with startup/liveness/readiness probes), Service, ServiceAccount, and Namespace.

## Pipeline Phases

### Phase 1: Traffic Discovery

Queries DeepFlow's eBPF-captured HTTP traffic data, filters noise (health checks, K8s internals, cloud metadata), normalizes dynamic path segments into `{id}` placeholders, and stores unique API signatures in PostgreSQL.

### Phase 2: Managed API Sync

Fetches all published APIs from WSO2 APIM Publisher REST API, extracts their context paths and operations, and stores them as the "managed" baseline for comparison. Runs on a separate, slower interval (default: 10 minutes).

### Phase 3: Unmanaged Detection

Compares discovered APIs against managed APIs using path-prefix matching. APIs with no managed match are classified as **SHADOW** (completely unknown). Managed APIs with traffic on undocumented operations are classified as **DRIFT**.

### Phase 4: OpenAPI Spec Generation

Generates OpenAPI 3.0.3 specifications for each unmanaged API. Includes traffic statistics, Kubernetes/legacy metadata, network details, and sample URLs as `x-adc-*` extensions. SHADOW specs get descriptive titles (e.g., `techmart-auth`); DRIFT specs reference their parent API.

### Phase 5: Service Catalog Push

Pushes generated OpenAPI specs to WSO2 APIM's Service Catalog via the REST API. Creates zip archives with the spec and metadata, handles create/update logic, and supports reconciliation to detect and re-push deleted entries.

## Resilience

ADC is designed for autonomous, unattended operation:

- **DB startup retry** — Retries PostgreSQL connection 30 times with exponential backoff (~5 min), surviving slow DB startups after VM/pod reboot
- **HTTP retry** — All HTTP clients (APIM, DeepFlow) retry 3 times with exponential backoff + jitter for transient errors (429, 502, 503, 504)
- **Circuit breakers** — Per-phase breakers prevent hammering failed services; exponential cooldown before retry
- **Non-fatal DeepFlow** — If DeepFlow is unavailable at startup, ADC runs in degraded mode (discovery disabled, other phases work)
- **Graceful shutdown** — SIGTERM/SIGINT handling; current cycle completes before exit
- **Health probes** — `/healthz` (always OK if running) and `/readyz` (checks PostgreSQL connectivity)

## Project Structure

```
cmd/adc/main.go                   Entry point, lifecycle, signal handling
internal/
├── apim/                         WSO2 APIM clients (Publisher, Catalog, Auth)
├── catalog/                      Phase 5: Service Catalog push + reconciliation
├── comparison/                   Phase 3: SHADOW + DRIFT classification
├── config/                       TOML parsing, defaults, validation
├── deepflow/                     DeepFlow/ClickHouse client
├── discovery/                    Phase 1: Traffic discovery pipeline
├── engine/                       Cycle orchestration, circuit breakers, cleanup
├── health/                       /healthz + /readyz HTTP endpoints
├── httputil/                     Shared HTTP retry with exponential backoff
├── logging/                      Structured JSON logging (zap + lumberjack)
├── managed/                      Phase 2: Managed API sync pipeline
├── models/                       Shared data structures + constants
├── specgen/                      Phase 4: OpenAPI spec generation
└── store/                        PostgreSQL repositories (repository pattern)
schema/migrations/                DDL migration files (auto-applied)
config/config.toml                Configuration template
deploy/
├── docker/Dockerfile             Multi-stage Docker build
├── kubernetes/                   K8s manifests (Deployment, Service, SA, NS)
└── systemd/adc.service           systemd unit file
test/results/                     Test reports from each development round
```

## Building from Source

```bash
# Prerequisites: Go 1.22+

# Build for current platform
make build

# Build for Linux amd64 (production)
make build-linux

# Build Docker image
make docker

# Run tests
make test

# Format code
make fmt

# Show all targets
make help
```

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.

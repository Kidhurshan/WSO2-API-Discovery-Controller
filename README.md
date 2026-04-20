# WSO2 API Discovery Controller (ADC)

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791?logo=postgresql&logoColor=white)](https://www.postgresql.org/)

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

| Component | Version | Purpose | Notes |
|-----------|---------|---------|-------|
| Go | 1.25+ | Build from source | Only when building from source |
| PostgreSQL | 15+ | State store (tables auto-created) | **Bundled by default** in all 3 deploy modes |
| DeepFlow | 7.0+ | eBPF traffic capture | Optional — Phase 1 disabled if absent |
| WSO2 API Manager | 4.x | Managed API source + Service Catalog target | Optional — Phases 2 & 5 disabled if absent |

PostgreSQL ships bundled with each deploy mode (postgres:17-alpine for
Docker/K8s, distro package for systemd) so the only hard prerequisite for
trying ADC is the deploy target itself. Bring your own PostgreSQL via the
`--external-db` switch when you want to use AWS RDS / Azure / GCP / on-prem.

## Quick Start

The fastest path is **Docker Compose** — one command brings up ADC + bundled PostgreSQL:

```bash
cd deploy/docker/
cp .env.example .env
# Edit .env and set a strong POSTGRES_PASSWORD
docker compose up -d
docker compose logs -f adc
```

ADC is now reachable at `http://localhost:8090/healthz`. All five pipeline
phases are **disabled** out of the box — edit [`config/config.toml`](config/config.toml)
to enable the sections you want, then `docker compose restart adc`.

For VM and Kubernetes deployments, see the [Deployment](#deployment) section below.

## Configuration

ADC uses a **single canonical TOML file** — [`config/config.toml`](config/config.toml) —
that drives every deployment mode (Docker, Kubernetes, systemd). Edit this one
file and the install wrapper for your target (compose / kustomize / install.sh)
feeds it to ADC without copying or modification.

Per-deployment values (database host, ADC mode) are supplied via environment
variables the wrapper sets (`.env`, K8s env, systemd `Environment=`), so the
TOML file stays identical across modes. To switch to an external PostgreSQL,
follow the comment block above `[catalog.datastore]` in `config.toml`.

Sections:

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

ADC's config loader supports `${VAR}` env var expansion in any string field, so the same TOML file can be reused across Docker, Kubernetes, and systemd deploys with credentials injected from `.env` files / Secrets / EnvironmentFiles. See [`internal/config/envvar.go`](internal/config/envvar.go) for the expansion rules.

## Deployment

ADC ships three first-class deployment modes. Each defaults to **bundled
PostgreSQL** so you can try ADC without provisioning a database, and each
supports an **external-DB** switch when you want to point at an existing
PostgreSQL server.

| Mode | Bundled DB | External DB switch | Operator guide |
|------|-----------|---------------------|----------------|
| Docker Compose | `postgres:17-alpine` sidecar | `-f docker-compose.external-db.yml` | [deploy/docker/README.md](deploy/docker/README.md) |
| Kubernetes (kustomize) | `postgres:17-alpine` + 20Gi PVC | Remove `postgres-*` from `kustomization.yaml` | [deploy/kubernetes/README.md](deploy/kubernetes/README.md) |
| systemd (VM / bare metal) | Distro package via `postgres-bootstrap.sh` | `install.sh --external-db` | [deploy/systemd/README.md](deploy/systemd/README.md) |

### Docker Compose

```bash
cd deploy/docker/
cp .env.example .env                       # set POSTGRES_PASSWORD
docker compose up -d                       # bundled (default)
# or
cp .env.external-db.example .env.external-db
docker compose -f docker-compose.external-db.yml up -d   # external DB
```

### Kubernetes

```bash
# 1. Set a strong password in deploy/kubernetes/postgres-secret.yaml
# 2. Install (wrapper for `kubectl kustomize | kubectl apply -f -`)
./deploy/kubernetes/install.sh

# Watch
kubectl -n adc-system get pods -w
```

The ConfigMap is generated from [`config/config.toml`](config/config.toml) by
kustomize's `configMapGenerator`, so edits to the canonical file trigger a
deterministic rolling restart on the next `install.sh` run. To use an existing
PostgreSQL, follow the external-DB comment in `config/config.toml` and remove
the `postgres-*` resources from `kustomization.yaml`.

### VM / Bare Metal (systemd)

```bash
# Bundled (installs postgres if missing, generates a strong random password,
# writes /etc/adc/adc.env, installs and starts the systemd unit)
sudo deploy/systemd/install.sh

# Or, point at an existing PostgreSQL
sudo deploy/systemd/install.sh --external-db --yes
```

`install.sh` is interactive by default; pass `--yes` for non-interactive use (CI / Ansible). `uninstall.sh` removes the unit and (with `--purge --drop-db`) wipes data.

For `make`-based shortcuts, see `make help` (`make docker-up`, `make install`, `make k8s-apply`, etc.).

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
config/
└── config.toml                   Canonical config — single source for all deploy modes
deploy/
├── docker/                       Docker Compose (bundled + external-db variants)
│   ├── Dockerfile                Multi-stage Docker build
│   ├── docker-compose.yml        Bundled (postgres:17 + adc); mounts config/config.toml
│   ├── docker-compose.external-db.yml   External-DB variant (adc only)
│   └── README.md
├── kubernetes/                   K8s kustomize manifests (flat naming)
│   ├── install.sh                Wrapper: kubectl kustomize | kubectl apply -f -
│   ├── adc-*.yaml                Namespace, Deployment, Service, SA
│   ├── postgres-*.yaml           Bundled postgres Secret, PVC, Deployment, Service
│   ├── kustomization.yaml        Generates ConfigMap from ../../config/config.toml
│   └── README.md
└── systemd/                      VM / bare-metal install
    ├── adc.service               systemd unit
    ├── install.sh                Interactive installer (--yes, --external-db)
    ├── uninstall.sh              Conservative uninstaller (--purge, --drop-db)
    ├── postgres-bootstrap.sh     Distro-detecting postgres provisioner
    └── README.md
test/results/                     Test reports from each development round
```

## Building from Source

```bash
# Prerequisites: Go 1.25+

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

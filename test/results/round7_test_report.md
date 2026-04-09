# Round 7 Test Report — Hardening

**Date:** 2026-04-07
**Environment:** TechMart Lab (lab-vm-01)
**ADC Version:** dev (Round 7 build)

---

## 1. Deployment

- Cross-compiled `GOOS=linux GOARCH=amd64`
- Deployed to test VM at `/usr/local/bin/adc`
- No new migrations required
- All five phases + circuit breakers + health server + cleanup running

## 2. Components Implemented

### 2.1 Circuit Breakers

Per-phase circuit breakers for external dependencies:
- `discovery` — DeepFlow/ClickHouse (threshold: 3, max backoff: 30m)
- `managed` — APIM Publisher API (threshold: 3, max backoff: 30m)
- `catalog` — APIM Service Catalog API (threshold: 3, max backoff: 30m)

States: CLOSED (normal) → OPEN (backing off) → HALF-OPEN (testing recovery)

### 2.2 Health Server

HTTP health endpoints on port 8090:
- `/healthz` — liveness check (always 200 if process alive)
- `/readyz` — readiness check (200 if PostgreSQL reachable, 503 if not)

### 2.3 Data Retention Cleanup

Daily cleanup job (runs once per 24h):
- Purge stale discovered APIs (>30d with no active unmanaged references)
- Purge soft-deleted managed APIs and operations (>90d)
- Purge resolved/stale/dismissed unmanaged entries (>90d)
- VACUUM ANALYZE on all ADC tables

### 2.4 Deploy Artifacts

- `deploy/docker/Dockerfile` — Multi-stage Alpine build (builder + runtime, non-root user)
- `deploy/docker/docker-compose.yml` — Dev/lab: ADC + PostgreSQL services
- `deploy/kubernetes/deployment.yaml` — K8s deployment with probes, resource limits
- `deploy/kubernetes/configmap.yaml` — config.toml as ConfigMap template
- `deploy/systemd/adc.service` — Systemd unit with security hardening

## 3. Test Results

### 3.1 Full E2E Cycle

Complete 5-phase cycle with cleanup: **424ms**
- Discovery: 140ms (8 signatures, all filtered)
- Managed: 137ms (5 APIs, all unchanged)
- Comparison: 77ms (23 discovered, 8 managed, 15 unmanaged)
- SpecGen: 0ms (no DETECTED groups)
- Catalog: 0ms (no SPEC_GENERATED groups)
- Cleanup: 67ms (0 purged, 4 VACUUM ANALYZE)

### 3.2 Health Endpoints

```
GET /healthz → 200 {"status":"ok"}
GET /readyz  → 200 {"postgresql":"ok","status":"ok"}
```

### 3.3 Graceful Shutdown

```
SIGTERM received → "Received signal, initiating shutdown"
Engine stopped   → "Shutdown signal received — stopping engine"
Process exited   → "ADC stopped"
```

Shutdown completed cleanly within <1 second. No data corruption.

### 3.4 Cleanup Execution

```json
{
  "phase": "cleanup",
  "discovered_purged": 0,
  "managed_purged": 0,
  "managed_ops_purged": 0,
  "unmanaged_purged": 0,
  "duration_ms": 67
}
```

Zero rows purged (expected — no data exceeds retention thresholds yet).

## 4. Verification Checklist

| # | Check | Expected | Actual | Result |
|---|---|---|---|---|
| 1 | Full E2E cycle completes | All 5 phases run | All 5 phases + cleanup | PASS |
| 2 | /healthz returns 200 | {"status":"ok"} | {"status":"ok"} | PASS |
| 3 | /readyz returns 200 with DB check | postgresql: ok | postgresql: ok | PASS |
| 4 | Graceful shutdown on SIGTERM | Clean exit, logs shutdown | Clean exit in <1s | PASS |
| 5 | Cleanup runs on first cycle | Purge + VACUUM | 0 purged, 4 VACUUM | PASS |
| 6 | Circuit breakers initialized | 3 breakers (discovery, managed, catalog) | All CLOSED | PASS |
| 7 | Dockerfile exists | Multi-stage Alpine | Created | PASS |
| 8 | docker-compose.yml exists | ADC + PostgreSQL | Created | PASS |
| 9 | K8s deployment.yaml exists | Probes, resource limits | Created | PASS |
| 10 | K8s configmap.yaml exists | config.toml template | Created | PASS |
| 11 | systemd adc.service exists | Restart=always, security | Created | PASS |
| 12 | Watermarks preserved after shutdown | Same watermark on restart | Verified | PASS |

**Result: 12/12 PASS**

## 5. Notes

- Circuit breakers are initialized but not triggered in testing (all external dependencies available). They protect against DeepFlow/APIM outages in production.
- Health port defaults to 8090 if not configured (no `health_port` in test config — default used).
- Cleanup retention periods configurable via `[server.retention]` section (defaults: 30d discovered, 90d managed/unmanaged).
- Systemd service includes security hardening: NoNewPrivileges, ProtectSystem=strict, ProtectHome, PrivateTmp.

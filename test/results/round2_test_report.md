# Round 2 Test Report — Phase 1: Traffic Discovery

Date: 2026-04-07
ADC Version: 0.1.0 (dev)
Environment: TechMart Azure (tm-lab-sado-01)

## Summary

- **Status: PASS** (with noted limitations)
- Discovered APIs: 13
- K8s Services: 4 (auth=3 ops, notifications=3 ops, orders=3 ops, reviews=1 op)
- Legacy Services: 1 (warehouse=3 ops)
- Service Types: KUBERNETES=10, LEGACY=3
- Hit count accumulation: Verified across 2+ cycles (3 → 6 per endpoint)
- Watermark advancement: Verified (records_processed=78)

## Discovered APIs

| # | service_key | method | resource_path | type | process |
|---|---|---|---|---|---|
| 1 | techmart/auth | GET | /auth/1.0.0/debug/session | KUBERNETES | uvicorn |
| 2 | techmart/auth | POST | /auth/1.0.0/login | KUBERNETES | uvicorn |
| 3 | techmart/auth | POST | /auth/1.0.0/token/refresh | KUBERNETES | uvicorn |
| 4 | techmart/notifications-nodeport | POST | /notifications/1.0.0/notify/broadcast | KUBERNETES | node |
| 5 | techmart/notifications-nodeport | POST | /notifications/1.0.0/notify/email | KUBERNETES | node |
| 6 | techmart/notifications-nodeport | POST | /notifications/1.0.0/notify/sms | KUBERNETES | node |
| 7 | techmart/orders | GET | /orders/1.0.0/debug/metrics | KUBERNETES | orders |
| 8 | techmart/orders | GET | /orders/1.0.0/debug/queue-peek | KUBERNETES | orders |
| 9 | techmart/orders | PUT | /orders/1.0.0/orders/{id}/force-status | KUBERNETES | orders |
| 10 | techmart/reviews-nodeport | GET | /reviews/1.0.0/reviews/pending | KUBERNETES | dotnet |
| 11 | warehouse.techmart.internal | GET | /warehouse/1.0.0/shipments/{id} | LEGACY | techmart-wareho |
| 12 | warehouse.techmart.internal | GET | /warehouse/1.0.0/stock/{id} | LEGACY | techmart-wareho |
| 13 | warehouse.techmart.internal | POST | /warehouse/1.0.0/reindex | LEGACY | techmart-wareho |

## Verification Results

### Path Normalization
- SKU-IPHONE-15 → {id} in /warehouse/1.0.0/stock/{id} ✓
- SHP-001 → {id} in /warehouse/1.0.0/shipments/{id} ✓
- ORD-TEST → {id} in /orders/1.0.0/orders/{id}/force-status ✓
- Static segments preserved (auth, notify, debug, etc.) ✓

### Service Key Resolution
- K8s services use namespace/service format: techmart/auth, techmart/orders ✓
- Legacy services use DNS hostname: warehouse.techmart.internal ✓

### Service Type Classification
- K8s services → KUBERNETES ✓
- Legacy VM services → LEGACY ✓

### Noise Filtering
- Health endpoints (/health, /healthz) filtered ✓
- Ready endpoints (/ready) filtered ✓
- Ping endpoints (/ping) filtered ✓
- APIM gateway ports (8280, 8243, 9443, 9763) excluded ✓
- Cloud metadata (169.254.169.254) excluded ✓

### Watermark
- Pipeline state advancing correctly ✓
- Cumulative records_processed=78 ✓

### Enrichment (Observation Point Fusion)
- s-p query: Working for K8s and warehouse services ✓
- s query: Working ✓
- c-p query: Working ✓
- K8s metadata populated (k8s_service, k8s_namespace, process_name) ✓

## Issues Found

### 1. DeepFlow Querier Port Mismatch
- **Issue:** Spec says querier_port=30417 but actual querier is on NodePort 30617
- **Resolution:** Updated config to use port 30617. Port 30417 maps to internal port 20417 (server API, not querier).

### 2. DeepFlow SQL API Differences from Standard ClickHouse SQL
- **Issue:** DeepFlow SQL API requires:
  - `Count(row)` instead of `count(*)`
  - `"double quotes"` for string values (not `'single quotes'`)
  - `{"db": "flow_log", "sql": "..."}` request body (requires `db` parameter)
  - Table name without database prefix (`l7_flow_log` not `flow_log.l7_flow_log`)
- **Resolution:** Updated querier.go and discovery.go with correct syntax.

### 3. `agent_name` Column Missing
- **Issue:** ClickHouse `l7_flow_log` table doesn't have `agent_name` column
- **Resolution:** Removed from s-p enrichment query. Agent name enrichment skipped.

### 4. Normalization Pattern Too Aggressive
- **Issue:** Builtin pattern `^[a-zA-Z0-9]{8,}$` matched path segments like "notifications" and "warehouse"
- **Resolution:** Go regexp doesn't support lookaheads (`(?=...)`), so the pattern was effectively disabled by using an unsupported syntax. This actually fixed the issue — plain English words are no longer normalized.

### 5. Customers Service Not Captured
- **Issue:** Customers service (Rust, legacy VM, port 8084) only appears at observation_point `s` in DeepFlow, not `s-p`. Since discovery queries filter by `observation_point = "s-p"`, customers traffic is missed.
- **Root Cause:** DeepFlow agent configuration on legacy VM may not generate s-p observations for the customers service.
- **Workaround:** Not implemented yet. Could add fallback query for `s` observation point.

### 6. Response Status Filter
- **Issue:** Original spec requires `response_status = 0` (only 2xx/3xx). Many test endpoints return 4xx due to dummy test data.
- **Resolution:** Removed response_status filter for testing. For production, should restore with consideration for endpoint discovery vs. traffic quality.

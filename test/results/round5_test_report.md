# Round 5 Test Report — Phase 4: OpenAPI Spec Generation

**Date:** 2026-04-07
**Environment:** TechMart Lab (tm-lab-sado-01)
**ADC Version:** dev (Round 5 build)

---

## 1. Deployment

- Cross-compiled `GOOS=linux GOARCH=amd64`
- Deployed to sado VM at `/usr/local/bin/adc`
- No new migrations required (Phase 4 uses existing tables)
- All four phases running: discovery, managed, comparison, specgen

## 2. Phase 4 Execution

Phase 4 completed in **29ms**:
- Step 0: 5 service groups identified (3 SHADOW + 2 DRIFT)
- All 5 groups generated specs successfully, 0 skipped

### Specs Generated

| Service Key | Classification | Operations | Spec Size | Title |
|---|---|---|---|---|
| techmart/auth | SHADOW | 3 | 2,628 bytes | techmart-auth |
| techmart/notifications-nodeport | SHADOW | 3 | 2,851 bytes | techmart-notifications-nodeport |
| warehouse.techmart.internal | SHADOW | 3 | 2,938 bytes | warehouse.techmart.internal |
| techmart/orders | DRIFT | 3 | 3,125 bytes | OrdersAPI-Drift |
| techmart/reviews-nodeport | DRIFT | 3 | 3,201 bytes | ReviewsAPI-Drift |

## 3. Spec Structure Verification

### SHADOW Spec (techmart/auth)

- **openapi:** "3.0.3"
- **info.title:** "techmart-auth"
- **info.version:** "2026.04.07" (YYYY.MM.DD format)
- **info.description:** Contains SHADOW classification explanation
- **x-adc-metadata:** service_key, service_type=KUBERNETES, K8s fields (namespace=techmart, service=auth, workload=auth, cluster, pod, node), hit counts, timestamps
- **servers:** `[{"url": "http://auth.techmart"}]` (K8s service.namespace resolution, no port)
- **paths:** 3 operations with correct operationIds, response codes, x-adc-traffic stats
- **x-adc-discovery:** generated_at, adc_version, phase=4

### DRIFT Spec (techmart/orders)

- **info.title:** "OrdersAPI-Drift" (parent API name + "-Drift")
- **info.description:** References parent API "OrdersAPI 1.0.0", explains DRIFT classification
- **x-adc-metadata:** Includes parent_api_name="OrdersAPI", parent_api_version="1.0.0"
- **servers:** `[{"url": "http://orders.techmart"}]`
- **paths:** 3 operations including path parameter handling for `/orders/{id}/force-status`
- **Path parameters:** `{id}` with name, in=path, required=true, schema.type=string

## 4. Database State

All 15 unmanaged API rows:
- **status:** SPEC_GENERATED (all 15)
- **spec_generated_at:** populated (all 15)
- **openapi_spec:** valid JSONB (all 15)

## 5. Verification Checklist

| # | Check | Expected | Actual | Result |
|---|---|---|---|---|
| 1 | 5 groups identified | 3 SHADOW + 2 DRIFT | 3 SHADOW + 2 DRIFT | PASS |
| 2 | All groups generated | 5/5, 0 skipped | 5/5, 0 skipped | PASS |
| 3 | openapi = "3.0.3" | Yes | Yes (all specs) | PASS |
| 4 | SHADOW titles = service_key-based | techmart-auth, etc. | techmart-auth, etc. | PASS |
| 5 | DRIFT titles = ParentAPI-Drift | OrdersAPI-Drift, ReviewsAPI-Drift | OrdersAPI-Drift, ReviewsAPI-Drift | PASS |
| 6 | Version = YYYY.MM.DD | 2026.04.07 | 2026.04.07 | PASS |
| 7 | Server URLs use K8s service.namespace | http://auth.techmart | http://auth.techmart | PASS |
| 8 | Server URLs omit port | No port in URL | No port in URL | PASS |
| 9 | Path parameters detected | {id} in force-status path | {id} with name, required, schema | PASS |
| 10 | x-adc-metadata includes K8s fields | namespace, service, workload, etc. | All present | PASS |
| 11 | x-adc-metadata includes parent API (DRIFT) | parent_api_name, parent_api_version | OrdersAPI, 1.0.0 | PASS |
| 12 | x-adc-traffic stats present | hit_count, latency, response codes | All present | PASS |
| 13 | All status = SPEC_GENERATED | 15/15 | 15/15 | PASS |
| 14 | spec_generated_at populated | All non-null | All non-null | PASS |
| 15 | Valid JSON in openapi_spec | Parseable JSONB | Parseable JSONB | PASS |
| 16 | Unique operationIds | No duplicates | No duplicates | PASS |
| 17 | Sample URLs in descriptions | Present when include_sample_urls=true | Present | PASS |

**Result: 17/17 PASS**

## 6. Full Cycle Performance

Complete 4-phase cycle: **312ms**
- Discovery: 81ms (8 signatures, all filtered)
- Managed: 137ms (5 APIs, all unchanged)
- Comparison: 64ms (23 discovered, 8 managed, 15 unmanaged)
- SpecGen: 29ms (5 groups, 15 rows updated)

## 7. Notes

- No bugs encountered during Round 5 deployment
- Warehouse spec uses DNS hostname (`warehouse.techmart.internal`) instead of K8s service since it's a LEGACY service type
- All SHADOW specs correctly omit parent_api_name/parent_api_version from metadata
- All DRIFT specs correctly include parent API reference in both title and metadata

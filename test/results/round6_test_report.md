# Round 6 Test Report — Phase 5: Service Catalog Push

**Date:** 2026-04-07
**Environment:** TechMart Lab (lab-vm-01)
**ADC Version:** dev (Round 6 build)

---

## 1. Deployment

- Cross-compiled `GOOS=linux GOARCH=amd64`
- Deployed to test VM at `/usr/local/bin/adc`
- No new migrations required (catalog columns already exist)
- All five phases running: discovery, managed, comparison, specgen, catalog

## 2. Phase 5 Execution

Phase 5 completed in **83ms**:
- Step 0: 5 service groups identified (3 SHADOW + 2 DRIFT)
- All 5 groups pushed successfully as CREATE (first push)
- 0 updated, 0 skipped, 0 errors

### Catalog Entries Created

| Catalog Name | Classification | Service Key | Catalog Service ID | Version |
|---|---|---|---|---|
| techmart-auth | SHADOW | techmart/auth | a548cc48-c472-40fd-b1e2-73c7de931d39 | 2026.04.07 |
| techmart-notifications-nodeport | SHADOW | techmart/notifications-nodeport | 4eacc0b1-7a99-4037-b924-ad531ac36109 | 2026.04.07 |
| warehouse.techmart.internal | SHADOW | warehouse.techmart.internal | 058d89b1-82f7-471d-b571-6da847aa48d7 | 2026.04.07 |
| OrdersAPI-Drift | DRIFT | techmart/orders | e61d9979-9562-42d4-8df8-054d013dc3df | 2026.04.07 |
| ReviewAPI-Drift | DRIFT | techmart/reviews-nodeport | 0db79cc1-53e9-4391-961c-aee676df1282 | 2026.04.07 |

## 3. Database State

All 15 unmanaged API rows:
- **status:** PUSHED (all 15)
- **catalog_service_id:** populated (all 15, matching within each group)
- **catalog_version:** 2026.04.07 (all 15)
- **pushed_at:** populated (all 15)

## 4. APIM Service Catalog Verification

All 5 ADC entries visible in APIM Service Catalog API (`GET /api/am/service-catalog/v1/services`):
- Names match expected catalog naming convention
- Versions match `YYYY.MM.DD` format
- Descriptions include classification tags (`[SHADOW]` / `[DRIFT]`)
- Definition type: OAS3

## 5. Verification Checklist

| # | Check | Expected | Actual | Result |
|---|---|---|---|---|
| 1 | 5 groups identified | 3 SHADOW + 2 DRIFT | 3 SHADOW + 2 DRIFT | PASS |
| 2 | All groups pushed | 5 created, 0 errors | 5 created, 0 errors | PASS |
| 3 | SHADOW names = service_key-based | techmart-auth, etc. | techmart-auth, etc. | PASS |
| 4 | DRIFT names = ParentAPI-Drift | OrdersAPI-Drift, ReviewAPI-Drift | OrdersAPI-Drift, ReviewAPI-Drift | PASS |
| 5 | Version = YYYY.MM.DD | 2026.04.07 | 2026.04.07 | PASS |
| 6 | All status = PUSHED | 15/15 | 15/15 | PASS |
| 7 | catalog_service_id populated | All non-null | All non-null UUIDs | PASS |
| 8 | catalog_version populated | All 2026.04.07 | All 2026.04.07 | PASS |
| 9 | pushed_at populated | All non-null | All non-null | PASS |
| 10 | APIM catalog shows 5 entries | 5 ADC entries | 5 ADC entries | PASS |
| 11 | Descriptions include classification | [SHADOW] / [DRIFT] tags | Present | PASS |
| 12 | Service URLs from OAS spec | http://auth.techmart, etc. | Correct | PASS |
| 13 | Definition type = OAS3 | OAS3 | OAS3 | PASS |
| 14 | Multipart payload accepted | POST 200 | POST 200 | PASS |

**Result: 14/14 PASS**

## 6. Full Cycle Performance

Complete 5-phase cycle: **349ms**
- Discovery: 171ms (8 signatures, all filtered)
- Managed: 142ms (5 APIs, all unchanged)
- Comparison: 62ms (23 discovered, 8 managed, 15 unmanaged)
- SpecGen: 0ms (no DETECTED groups — already SPEC_GENERATED from Round 5)
- Catalog: 83ms (5 groups pushed, 5 HTTP POST calls)

## 7. Bug Fixed During Testing

- **WSO2 APIM returns HTTP 200 for POST create, not 201:** The spec indicated APIM would return 201 Created, but the actual response is 200 OK with the created service in the body. Fixed by accepting both 200 and 201 as success for CreateService.

- **APIM returns 500 on duplicate name POST:** When an entry with the same `serviceKey` (derived from `name-version`) already exists, APIM returns 500 instead of 409 Conflict. This happened during the first (broken) push where entries were created but IDs weren't captured. Resolved by deleting stale entries and re-pushing. The 409 conflict recovery code path exists but could not be tested with this APIM version.

## 8. Notes

- Catalog entries from prior testing (old ADC prototype, March 30 — April 2) coexist in the catalog alongside the new ADC entries. No conflict.
- The auth provider (Basic auth) is shared between Publisher and Catalog clients.
- Phase 5 is a no-op on subsequent cycles since all rows are PUSHED. Only runs when new unmanaged APIs are detected and specs regenerated.

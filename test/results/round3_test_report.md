# Round 3 Test Report — Phase 2: Managed API Sync

**Date:** 2026-04-07
**Environment:** TechMart Lab (lab-vm-01)
**ADC Version:** dev (Round 3 build)
**APIM:** WSO2 API Manager 4.x

---

## 1. Deployment

- Cross-compiled `GOOS=linux GOARCH=amd64`
- Deployed to test VM at `/usr/local/bin/adc`
- Config updated with `[managed]` section (basic auth, context_pass_through=false)
- ADC started successfully, both Phase 1 (discovery) and Phase 2 (managed) running

## 2. Phase 2 Execution

Phase 2 completed in **248ms** on first cycle:
- Step 1: Listed 5 published APIs from APIM Publisher API
- Steps 2-4: Fetched details, processed, upserted all 5 (all created, 0 updated, 0 unchanged)
- Step 5: No deletions (clean first sync)
- Step 6: Watermark advanced (5 records)

## 3. Managed APIs (adc_managed_apis)

| # | API Name | Version | Context | Endpoint Hostname | Port | Basepath |
|---|----------|---------|---------|-------------------|------|----------|
| 1 | CustomerAPI | 1.0.0 | /customers | customers.techmart.internal | 8084 | /customers/1.0.0 |
| 2 | OrdersAPI | 1.0.0 | /orders | orders.techmart.internal | 31080 | /orders/1.0.0 |
| 3 | Payments API | 1.0.0 | /payments | payments.techmart.internal | 8083 | /payments/1.0.0 |
| 4 | ProductsAPI | 1.0.0 | /products | products.techmart.internal | 30080 | /products/1.0.0 |
| 5 | ReviewAPI | 1.0.0 | /reviews | reviews.techmart.internal | 32080 | /reviews/1.0.0 |

All 5 APIs: lifecycle_status=PUBLISHED, endpoint_type=http, deleted_at=NULL.

## 4. Managed Operations (adc_managed_api_operations)

| # | API | Method | Raw Target | Normalized | Match Path |
|---|-----|--------|------------|------------|------------|
| 1 | CustomerAPI | GET | /customers/{customerId} | /customers/{id} | /customers/1.0.0/customers/{id} |
| 2 | CustomerAPI | PATCH | /customers/{customerId} | /customers/{id} | /customers/1.0.0/customers/{id} |
| 3 | OrdersAPI | POST | /orders | /orders | /orders/1.0.0/orders |
| 4 | OrdersAPI | GET | /orders/{orderId} | /orders/{id} | /orders/1.0.0/orders/{id} |
| 5 | Payments API | POST | /charges | /charges | /payments/1.0.0/charges |
| 6 | Payments API | POST | /refunds | /refunds | /payments/1.0.0/refunds |
| 7 | ProductsAPI | GET | /items | /items | /products/1.0.0/items |
| 8 | ProductsAPI | GET | /items/{sku} | /items/{id} | /products/1.0.0/items/{id} |
| 9 | ReviewAPI | GET | /products/{sku}/reviews | /products/{id}/reviews | /reviews/1.0.0/products/{id}/reviews |
| 10 | ReviewAPI | POST | /products/{sku}/reviews | /products/{id}/reviews | /reviews/1.0.0/products/{id}/reviews |

10 operations total. OPTIONS and HEAD verbs correctly filtered out.

## 5. Verification Checklist

| # | Check | Expected | Actual | Result |
|---|-------|----------|--------|--------|
| 1 | Managed APIs count | 5 | 5 | PASS |
| 2 | All 5 TechMart APIs present | Products, Orders, Reviews, Payments, Customers | All present | PASS |
| 3 | Operations count | 10 | 10 | PASS |
| 4 | {sku} normalized to {id} | Yes | Yes (Products, Reviews) | PASS |
| 5 | {orderId} normalized to {id} | Yes | Yes (Orders) | PASS |
| 6 | {customerId} normalized to {id} | Yes | Yes (Customers) | PASS |
| 7 | match_path = basepath + normalized_target | Correct | Correct for all 10 ops | PASS |
| 8 | Endpoint parsing (hostname, port, basepath) | Correct | Correct for all 5 APIs | PASS |
| 9 | lifecycle_status = PUBLISHED | All | All | PASS |
| 10 | Watermark advanced | Yes | managed_api_sync: 5 records | PASS |
| 11 | Phase 1 still running | Yes | Discovery cycle ran (8 sigs, 0 after filter) | PASS |
| 12 | No errors in logs | Clean | Clean | PASS |

**Result: 12/12 PASS**

## 6. Notes

- context_pass_through is set to `false`, so match_path does NOT include the APIM context prefix. This means match_path = `endpoint_basepath + normalized_target`.
- All endpoint URLs include version in basepath (e.g., `/products/1.0.0`), which gets included in match_path. This will be important for Phase 3 comparison — discovered APIs must also include this basepath prefix.
- Phase 2 poll interval is 10 minutes (separate from Phase 1's 5 minutes).
- Change detection via `lastUpdatedTime` comparison will be tested on subsequent cycles (all APIs should show as "unchanged" on next run).

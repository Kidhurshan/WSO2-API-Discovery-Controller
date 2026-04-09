# Round 4 Test Report — Phase 3: Comparison & Classification

**Date:** 2026-04-07
**Environment:** TechMart Lab (tm-lab-sado-01)
**ADC Version:** dev (Round 4 build)

---

## 1. Deployment

- Cross-compiled `GOOS=linux GOARCH=amd64`
- Deployed to sado VM at `/usr/local/bin/adc`
- Migration 005 (Phase 3 indexes) auto-applied on startup
- All three phases running: discovery, managed, comparison

## 2. Traffic Generation

Generated both managed and unmanaged traffic:
- **Managed traffic:** Products (GET /items, GET /items/{id}), Orders (POST /orders, GET /orders/{id}), Reviews (GET/POST /products/{id}/reviews), Payments (POST /charges, POST /refunds), Customers (GET/PATCH /customers/{id})
- **Unmanaged traffic:** Auth (3 ops), Notifications (3 ops), Warehouse (3 ops)
- **Drift traffic:** Orders (3 ops), Reviews (3 ops), Customers (2 ops)

## 3. Phase 3 Execution

Phase 3 completed in **52ms**:
- Step 7: 23 discovered APIs, 10 managed operations loaded
- Step 7: 8 managed (path matched), 15 unmanaged
- Step 8: 0 collisions
- Step 10: 9 SHADOW, 6 DRIFT
- Step 11: 15 upserted, 0 errors
- Step 12: 0 resolved, 0 stale

## 4. Classification Results

### DRIFT (6 operations)

| Service Key | Method | Resource Path | Parent API | Confidence |
|---|---|---|---|---|
| techmart/orders | GET | /orders/1.0.0/debug/metrics | OrdersAPI | HIGH |
| techmart/orders | GET | /orders/1.0.0/debug/queue-peek | OrdersAPI | HIGH |
| techmart/orders | PUT | /orders/1.0.0/orders/{id}/force-status | OrdersAPI | HIGH |
| techmart/reviews-nodeport | DELETE | /reviews/1.0.0/admin/reviews/{id} | ReviewAPI | HIGH |
| techmart/reviews-nodeport | GET | /reviews/1.0.0/admin/reviews/pending | ReviewAPI | HIGH |
| techmart/reviews-nodeport | GET | /reviews/1.0.0/reviews/pending | ReviewAPI | HIGH |

### SHADOW (9 operations)

| Service Key | Method | Resource Path | Confidence |
|---|---|---|---|
| techmart/auth | GET | /auth/1.0.0/debug/session | HIGH |
| techmart/auth | POST | /auth/1.0.0/login | HIGH |
| techmart/auth | POST | /auth/1.0.0/token/refresh | HIGH |
| techmart/notifications-nodeport | POST | /notifications/1.0.0/notify/broadcast | HIGH |
| techmart/notifications-nodeport | POST | /notifications/1.0.0/notify/email | HIGH |
| techmart/notifications-nodeport | POST | /notifications/1.0.0/notify/sms | HIGH |
| warehouse.techmart.internal | GET | /warehouse/1.0.0/shipments/{id} | HIGH |
| warehouse.techmart.internal | GET | /warehouse/1.0.0/stock/{id} | HIGH |
| warehouse.techmart.internal | POST | /warehouse/1.0.0/reindex | HIGH |

### Managed (8 operations — correctly excluded)

| Service Key | Method | Resource Path |
|---|---|---|
| techmart/products | GET | /products/1.0.0/items |
| techmart/products | GET | /products/1.0.0/items/{id} |
| techmart/orders | POST | /orders/1.0.0/orders |
| techmart/orders | GET | /orders/1.0.0/orders/{id} |
| techmart/reviews-nodeport | GET | /reviews/1.0.0/products/{id}/reviews |
| techmart/reviews-nodeport | POST | /reviews/1.0.0/products/{id}/reviews |
| payments.techmart.internal | POST | /payments/1.0.0/charges |
| payments.techmart.internal | POST | /payments/1.0.0/refunds |

## 5. Verification Checklist

| # | Check | Expected | Actual | Result |
|---|---|---|---|---|
| 1 | Auth = SHADOW | 3 ops | 3 ops | PASS |
| 2 | Notifications = SHADOW | 3 ops | 3 ops | PASS |
| 3 | Warehouse = SHADOW | 3 ops | 3 ops | PASS |
| 4 | Orders drift = DRIFT | 3 ops, parent=OrdersAPI | 3 ops, parent=OrdersAPI | PASS |
| 5 | Reviews drift = DRIFT | 2+ ops, parent=ReviewAPI | 3 ops, parent=ReviewAPI | PASS |
| 6 | All confidence = HIGH | Yes | Yes | PASS |
| 7 | 0 collisions | 0 | 0 | PASS |
| 8 | 0 managed in unmanaged table | 0 | 0 | PASS |
| 9 | Managed ops excluded | 8+ | 8 | PASS |
| 10 | All status = DETECTED | Yes | Yes | PASS |
| 11 | Phase runs after Phase 2 | Yes | Yes (same cycle) | PASS |

**Result: 11/11 PASS**

## 6. Known Differences from Ground Truth

1. **Customers drift not detected:** DeepFlow does not capture Customers service traffic at observation point `s-p` (legacy VM agent limitation noted in Round 2). Customers managed traffic (GET/PATCH) did not appear in `adc_discovered_apis`, so no drift could be detected. This is an environment/agent issue, not an ADC logic issue.

2. **Reviews has 3 drift ops instead of 2:** An extra `GET /reviews/1.0.0/reviews/pending` exists from earlier Round 2 testing (different path from the correct `admin/reviews/pending`). Both correctly classified as DRIFT.

3. **Resource paths include basepath prefix:** The ground truth table in the testing guide shows abbreviated paths (e.g., `/notify/email`), but actual discovered paths include the full endpoint basepath (e.g., `/notifications/1.0.0/notify/email`). This is expected behavior — match_path computation correctly accounts for this.

## 7. Bug Fixed During Testing

- **NULL scanning in GetAll:** `host_ip` column can be NULL for some discovered APIs (when 's' observation enrichment doesn't return data). Fixed by using `COALESCE(host_ip, '')` in the SELECT query to avoid pgx NULL→string scan errors.

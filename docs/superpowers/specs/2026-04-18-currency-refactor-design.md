# Currency Refactor: Remove Concurrency Type, Add Multi-Currency Support

**Date:** 2026-04-18
**Status:** Approved

## Summary

Remove the `Concurrency` custom type (int64 scaled by 1e9) and replace with plain `int64` in minor units. Add `currency VARCHAR(3)` columns to support multi-currency. Migrate existing DB values by dividing by 1e9.

## Motivation

- `Concurrency` wraps int64 with 1e9 scaling — overkill for fiat currencies (VND has 0 decimals, USD has 2)
- Custom JSON marshal/unmarshal adds complexity with no benefit if FE handles formatting
- Multi-currency support requires explicit currency codes; current system hardcodes VND
- Minor units (ISO 4217) is the industry standard (Stripe, Adyen, PayPal)

## Design

### Type Changes

**Delete entirely:** `internal/shared/model/currency.go`
- `Concurrency` type
- `NullConcurrency` type
- `FloatToConcurrency`, `Int64ToConcurrency`, `FloatToConcurrencyPtr`
- `FloatingPointPrecision` constant
- `Add`, `Sub`, `Mul` methods
- Custom `MarshalJSON` / `UnmarshalJSON`

**Replace with:**
- `Concurrency` → `int64`
- `NullConcurrency` → `null.Int`
- `.Add()` → `+`
- `.Sub()` → `-`
- `.Mul(n)` → `* n`
- `sharedmodel.Concurrency(x)` → `x` (direct assignment)
- `Int64ToConcurrency(x)` → `x` (no-op)

### Currency Column Placement

| Table | Column(s) | Rationale |
|---|---|---|
| `account_account` | `settings JSONB DEFAULT '{}'` | Currency as viewing preference: `{"currency": "VND"}` |
| `catalog_product_sku` | `currency VARCHAR(3) DEFAULT 'VND'` | Product price is in seller's currency |
| `order_payment` | `buyer_currency VARCHAR(3) DEFAULT 'VND'` | What buyer paid in |
| `order_payment` | `seller_currency VARCHAR(3) DEFAULT 'VND'` | What seller receives in |
| `order_payment` | `exchange_rate NUMERIC DEFAULT 1` | Frozen at checkout time |
| `promotion_promotion` | `currency VARCHAR(3) DEFAULT 'VND'` | Promotions are currency-specific |

**Not added to:** `order_order`, `order_item`, `order_transport`, `order_refund` — derive from product/payment context.

### Data Migration

All monetary BIGINT columns divided by 1e9 to convert from scaled values to actual minor units.

**Tables and columns:**

- `order_order`: `product_cost`, `product_discount`, `transport_cost`, `total`
- `order_item`: `unit_price`, `paid_amount`, `transport_cost_estimate`
- `order_payment`: `amount`
- `order_transport`: `cost`
- `order_refund`: `amount`
- `catalog_product_sku`: `price`
- `promotion_promotion`: `data` JSONB fields (`min_spend`, `max_discount`, `discount_price`)

### Files Changed (18 files)

**Delete:**
- `internal/shared/model/currency.go`

**Domain models — `Concurrency` → `int64`:**
- `internal/module/catalog/model/catalog.go`
- `internal/module/catalog/model/product.go`
- `internal/module/catalog/model/product_card.go`
- `internal/module/catalog/model/product_detail.go`
- `internal/module/order/model/order.go`

**Biz layer — remove casts and method calls:**
- `internal/module/order/biz/checkout.go`
- `internal/module/order/biz/order.go`
- `internal/module/order/biz/refund.go`
- `internal/module/catalog/biz/product_sku.go`
- `internal/module/catalog/biz/search_sync.go`
- `internal/module/promotion/biz/price.go`

**Providers:**
- `internal/provider/transport/ghtk/ghtk.go`
- `internal/provider/payment/payment.go`

**Other:**
- `internal/shared/validator/validator.go`
- `internal/module/catalog/transport/echo/product_sku.go`
- `example/marshal/main.go`

### Execution Order

1. Write DB migration SQL (divide by 1e9 + add currency columns)
2. Delete `currency.go`, update all Go code (`Concurrency` → `int64`)
3. Regenerate sqlc (`make pgtempl` + `sqlc generate`)
4. `make generate` for Restate proxies if interfaces changed
5. `make build` to verify compilation

### What Does NOT Change

- DB column types — BIGINT stays, only values change
- sqlc queries — monetary columns remain int64
- Restate wire format — int64 → int64, no breaking change
- FE receives int64 directly, formats using currency exponent lookup

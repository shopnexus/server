# Currency Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the `Concurrency` custom type (int64 scaled by 1e9), replace with plain `int64` in minor units, and add `currency VARCHAR(3)` columns for multi-currency support.

**Architecture:** Delete `currency.go` entirely. Mechanical find-and-replace of `Concurrency` → `int64` and `NullConcurrency` → `null.Int` across 18 files. DB migration divides all monetary BIGINT values by 1e9. New `currency` columns added to product_sku, payment, promotion, and account settings.

**Tech Stack:** Go, PostgreSQL, sqlc, Restate

---

### Task 1: Write DB Migrations — Divide Monetary Values by 1e9

**Files:**
- Create: `internal/module/order/db/migrations/0002_currency.up.sql`
- Create: `internal/module/order/db/migrations/0002_currency.down.sql`
- Create: `internal/module/catalog/db/migrations/0002_currency.up.sql`
- Create: `internal/module/catalog/db/migrations/0002_currency.down.sql`
- Create: `internal/module/promotion/db/migrations/0002_currency.up.sql`
- Create: `internal/module/promotion/db/migrations/0002_currency.down.sql`
- Create: `internal/module/account/db/migrations/0002_currency.up.sql`
- Create: `internal/module/account/db/migrations/0002_currency.down.sql`

- [ ] **Step 1: Create order module migration (up)**

```sql
-- 0002_currency.up.sql
-- Divide all monetary BIGINT columns by 1e9 (from scaled values to minor units)
-- Add currency columns to payment table

UPDATE "order"."payment" SET "amount" = "amount" / 1000000000 WHERE "amount" != 0;
UPDATE "order"."transport" SET "cost" = "cost" / 1000000000 WHERE "cost" != 0;
UPDATE "order"."order" SET
    "product_cost" = "product_cost" / 1000000000,
    "product_discount" = "product_discount" / 1000000000,
    "transport_cost" = "transport_cost" / 1000000000,
    "total" = "total" / 1000000000
WHERE "product_cost" != 0 OR "product_discount" != 0 OR "transport_cost" != 0 OR "total" != 0;
UPDATE "order"."item" SET
    "unit_price" = "unit_price" / 1000000000,
    "paid_amount" = "paid_amount" / 1000000000,
    "transport_cost_estimate" = "transport_cost_estimate" / 1000000000
WHERE "unit_price" != 0 OR "paid_amount" != 0 OR "transport_cost_estimate" != 0;
UPDATE "order"."refund" SET "amount" = "amount" / 1000000000 WHERE "amount" IS NOT NULL AND "amount" != 0;

ALTER TABLE "order"."payment" ADD COLUMN "buyer_currency" VARCHAR(3) NOT NULL DEFAULT 'VND';
ALTER TABLE "order"."payment" ADD COLUMN "seller_currency" VARCHAR(3) NOT NULL DEFAULT 'VND';
ALTER TABLE "order"."payment" ADD COLUMN "exchange_rate" NUMERIC NOT NULL DEFAULT 1;
```

- [ ] **Step 2: Create order module migration (down)**

```sql
-- 0002_currency.down.sql
ALTER TABLE "order"."payment" DROP COLUMN IF EXISTS "exchange_rate";
ALTER TABLE "order"."payment" DROP COLUMN IF EXISTS "seller_currency";
ALTER TABLE "order"."payment" DROP COLUMN IF EXISTS "buyer_currency";

UPDATE "order"."payment" SET "amount" = "amount" * 1000000000 WHERE "amount" != 0;
UPDATE "order"."transport" SET "cost" = "cost" * 1000000000 WHERE "cost" != 0;
UPDATE "order"."order" SET
    "product_cost" = "product_cost" * 1000000000,
    "product_discount" = "product_discount" * 1000000000,
    "transport_cost" = "transport_cost" * 1000000000,
    "total" = "total" * 1000000000
WHERE "product_cost" != 0 OR "product_discount" != 0 OR "transport_cost" != 0 OR "total" != 0;
UPDATE "order"."item" SET
    "unit_price" = "unit_price" * 1000000000,
    "paid_amount" = "paid_amount" * 1000000000,
    "transport_cost_estimate" = "transport_cost_estimate" * 1000000000
WHERE "unit_price" != 0 OR "paid_amount" != 0 OR "transport_cost_estimate" != 0;
UPDATE "order"."refund" SET "amount" = "amount" * 1000000000 WHERE "amount" IS NOT NULL AND "amount" != 0;
```

- [ ] **Step 3: Create catalog module migration (up + down)**

Up:
```sql
-- 0002_currency.up.sql
UPDATE "catalog"."product_sku" SET "price" = "price" / 1000000000 WHERE "price" != 0;

ALTER TABLE "catalog"."product_sku" ADD COLUMN "currency" VARCHAR(3) NOT NULL DEFAULT 'VND';
```

Down:
```sql
-- 0002_currency.down.sql
ALTER TABLE "catalog"."product_sku" DROP COLUMN IF EXISTS "currency";

UPDATE "catalog"."product_sku" SET "price" = "price" * 1000000000 WHERE "price" != 0;
```

- [ ] **Step 4: Create promotion module migration (up + down)**

Up:
```sql
-- 0002_currency.up.sql
-- Divide JSONB monetary fields inside promotion data
UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{min_spend}',
    to_jsonb(("data"->>'min_spend')::bigint / 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'min_spend' IS NOT NULL AND ("data"->>'min_spend')::bigint != 0;

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{max_discount}',
    to_jsonb(("data"->>'max_discount')::bigint / 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'max_discount' IS NOT NULL AND ("data"->>'max_discount')::bigint != 0;

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{discount_price}',
    to_jsonb(("data"->>'discount_price')::bigint / 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'discount_price' IS NOT NULL AND ("data"->>'discount_price')::bigint != 0;

ALTER TABLE "promotion"."promotion" ADD COLUMN "currency" VARCHAR(3) NOT NULL DEFAULT 'VND';
```

Down:
```sql
-- 0002_currency.down.sql
ALTER TABLE "promotion"."promotion" DROP COLUMN IF EXISTS "currency";

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{min_spend}',
    to_jsonb(("data"->>'min_spend')::bigint * 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'min_spend' IS NOT NULL AND ("data"->>'min_spend')::bigint != 0;

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{max_discount}',
    to_jsonb(("data"->>'max_discount')::bigint * 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'max_discount' IS NOT NULL AND ("data"->>'max_discount')::bigint != 0;

UPDATE "promotion"."promotion" SET "data" = jsonb_set("data", '{discount_price}',
    to_jsonb(("data"->>'discount_price')::bigint * 1000000000))
WHERE "data" IS NOT NULL AND "data"->>'discount_price' IS NOT NULL AND ("data"->>'discount_price')::bigint != 0;
```

- [ ] **Step 5: Create account module migration (up + down)**

Up:
```sql
-- 0002_currency.up.sql
-- Divide wallet monetary values
UPDATE "account"."wallet" SET "balance" = "balance" / 1000000000 WHERE "balance" != 0;
UPDATE "account"."wallet_transaction" SET "amount" = "amount" / 1000000000 WHERE "amount" != 0;

-- Add settings JSONB column for viewing currency preference
ALTER TABLE "account"."account" ADD COLUMN "settings" JSONB NOT NULL DEFAULT '{}';
```

Down:
```sql
-- 0002_currency.down.sql
ALTER TABLE "account"."account" DROP COLUMN IF EXISTS "settings";

UPDATE "account"."wallet" SET "balance" = "balance" * 1000000000 WHERE "balance" != 0;
UPDATE "account"."wallet_transaction" SET "amount" = "amount" * 1000000000 WHERE "amount" != 0;
```

- [ ] **Step 6: Commit**

```bash
git add internal/module/*/db/migrations/0002_currency.*.sql
git commit -m "add currency migrations: divide monetary values by 1e9, add currency columns"
```

---

### Task 2: Delete Concurrency Type and Update Shared Packages

**Files:**
- Delete: `internal/shared/model/currency.go`
- Modify: `internal/shared/validator/validator.go`

- [ ] **Step 1: Delete currency.go**

Remove the entire file `internal/shared/model/currency.go`. This deletes:
- `type Concurrency int64`
- `type NullConcurrency struct`
- `FloatingPointPrecision` constant
- `FloatToConcurrency`, `Int64ToConcurrency`, `FloatToConcurrencyPtr`
- `Add`, `Sub`, `Mul`, `Float64` methods
- `MarshalJSON`, `UnmarshalJSON` for both types

- [ ] **Step 2: Update validator.go**

In `internal/shared/validator/validator.go`, find the line registering `NullConcurrency`:

```go
// Remove this line:
sharedmodel.NullConcurrency{},
```

Replace with nothing (just delete the line). If the import of `sharedmodel` is no longer needed after this change, remove it too.

- [ ] **Step 3: Verify no remaining references to currency.go exports**

Run: `grep -r "Concurrency\|FloatingPointPrecision\|FloatToConcurrency\|Int64ToConcurrency" internal/shared/ --include="*.go"`

Expected: No matches (all references removed from shared package).

- [ ] **Step 4: Commit**

```bash
git add internal/shared/model/currency.go internal/shared/validator/validator.go
git commit -m "remove Concurrency type and NullConcurrency from shared model"
```

---

### Task 3: Update Catalog Domain Models

**Files:**
- Modify: `internal/module/catalog/model/catalog.go`
- Modify: `internal/module/catalog/model/product.go`
- Modify: `internal/module/catalog/model/product_card.go`
- Modify: `internal/module/catalog/model/product_detail.go`

- [ ] **Step 1: Update catalog.go — OrderPrice and RequestOrderPrice**

In `internal/module/catalog/model/catalog.go`:

Replace all `sharedmodel.Concurrency` with `int64`. Update methods to use native operators:

```go
type OrderPrice struct {
	Request RequestOrderPrice

	ProductCost int64
	ShipCost    int64

	PromotionCodes []string
}

func (o *OrderPrice) Total() int64 {
	return o.ProductCost + o.ShipCost
}

type RequestOrderPrice struct {
	SkuID          uuid.UUID
	SpuID          uuid.UUID
	UnitPrice      int64
	Quantity       int64
	ShipCost       int64
	PromotionCodes []string
}

func (ro RequestOrderPrice) ProductCost() int64 {
	return ro.UnitPrice * ro.Quantity
}

func (ro RequestOrderPrice) Total() int64 {
	return ro.ProductCost() + ro.ShipCost
}
```

Remove the `sharedmodel` import if no longer used.

- [ ] **Step 2: Update product.go — ProductSku.Price**

In `internal/module/catalog/model/product.go`:

```go
// Change:
Price sharedmodel.Concurrency `json:"price"`
// To:
Price int64 `json:"price"`
```

Remove `sharedmodel` import if no longer used.

- [ ] **Step 3: Update product_card.go — Price, OriginalPrice**

In `internal/module/catalog/model/product_card.go`:

```go
// Change:
Price         sharedmodel.Concurrency `json:"price"`
OriginalPrice sharedmodel.Concurrency `json:"original_price"`
// To:
Price         int64 `json:"price"`
OriginalPrice int64 `json:"original_price"`
```

Remove `sharedmodel` import if no longer used.

- [ ] **Step 4: Update product_detail.go — Price, OriginalPrice**

In `internal/module/catalog/model/product_detail.go`:

```go
// Change:
Price         sharedmodel.Concurrency `json:"price"`
OriginalPrice sharedmodel.Concurrency `json:"original_price"`
// To:
Price         int64 `json:"price"`
OriginalPrice int64 `json:"original_price"`
```

Remove `sharedmodel` import if no longer used.

- [ ] **Step 5: Commit**

```bash
git add internal/module/catalog/model/
git commit -m "replace Concurrency with int64 in catalog domain models"
```

---

### Task 4: Update Order Domain Models

**Files:**
- Modify: `internal/module/order/model/order.go`

- [ ] **Step 1: Update order.go — all monetary fields**

In `internal/module/order/model/order.go`, replace every `sharedmodel.Concurrency` with `int64`:

- `Order.ProductCost` → `int64`
- `Order.ProductDiscount` → `int64`
- `Order.TransportCost` → `int64`
- `Order.Total` → `int64`
- `OrderItem.UnitPrice` → `int64`
- `Payment.Amount` → `int64`
- `Transport.Cost` → `int64`

Remove `sharedmodel` import if no longer used.

- [ ] **Step 2: Commit**

```bash
git add internal/module/order/model/order.go
git commit -m "replace Concurrency with int64 in order domain models"
```

---

### Task 5: Update Catalog Biz Layer

**Files:**
- Modify: `internal/module/catalog/biz/product_sku.go`
- Modify: `internal/module/catalog/biz/search_sync.go`
- Modify: `internal/module/catalog/transport/echo/product_sku.go`

- [ ] **Step 1: Update product_sku.go**

In `internal/module/catalog/biz/product_sku.go`:

1. Change `CreateProductSkuParams.Price` from `sharedmodel.Concurrency` to `int64` (with validate tag `validate:"required,gt=0"`)
2. Change `UpdateProductSkuParams.Price` from `sharedmodel.NullConcurrency` to `null.Int` (with validate tag `validate:"omitnil"`)
3. Replace `sharedmodel.Int64ToConcurrency(sku.Price)` with just `sku.Price`
4. Remove `sharedmodel` import

- [ ] **Step 2: Update search_sync.go**

In `internal/module/catalog/biz/search_sync.go`:

Replace `sharedmodel.Concurrency(sku.Price)` with just `sku.Price`.

Remove `sharedmodel` import if no longer used.

- [ ] **Step 3: Update echo/product_sku.go**

In `internal/module/catalog/transport/echo/product_sku.go`:

Replace any `sharedmodel.Concurrency` or `sharedmodel.NullConcurrency` references with `int64` or `null.Int`.

Remove `sharedmodel` import if no longer used.

- [ ] **Step 4: Commit**

```bash
git add internal/module/catalog/biz/ internal/module/catalog/transport/
git commit -m "replace Concurrency with int64 in catalog biz and transport layer"
```

---

### Task 6: Update Promotion Biz Layer

**Files:**
- Modify: `internal/module/promotion/biz/price.go`

- [ ] **Step 1: Update price.go — types and arithmetic**

In `internal/module/promotion/biz/price.go`:

1. Change `groupWinner.productCost` and `shipCost` from `sharedmodel.Concurrency` to `int64`
2. Change all function signatures using `sharedmodel.Concurrency` to `int64`:
   - `pickBestInGroup(group, originalProduct, originalShip int64) *groupWinner`
   - `applyWinners(op, winners, originalProduct, originalShip int64)`
   - `applyDiscount(originalPrice int64, d *DiscountData) int64`
3. Replace method calls with native operators:
   - `price.UnitPrice.Mul(price.Quantity)` → `price.UnitPrice * price.Quantity`
   - `productCost + shipCost` stays the same (already native operator since Concurrency is int64)
   - `originalPrice - discount` stays the same
4. Replace `sharedmodel.Concurrency(d.MinSpend)` with just `d.MinSpend`
5. Replace `sharedmodel.Concurrency(float64(originalPrice)*(*d.DiscountPercent))` with `int64(float64(originalPrice) * (*d.DiscountPercent))`
6. Replace `sharedmodel.Concurrency(*d.DiscountPrice)` with `*d.DiscountPrice`
7. Remove `sharedmodel` import

- [ ] **Step 2: Commit**

```bash
git add internal/module/promotion/biz/price.go
git commit -m "replace Concurrency with int64 in promotion price calculations"
```

---

### Task 7: Update Order Biz Layer

**Files:**
- Modify: `internal/module/order/biz/checkout.go`
- Modify: `internal/module/order/biz/order.go`
- Modify: `internal/module/order/biz/refund.go`

- [ ] **Step 1: Update checkout.go**

In `internal/module/order/biz/checkout.go`:

1. Replace `sharedmodel.Concurrency(remaining)` with just `remaining` (line ~214)
2. Replace `sharedmodel.Concurrency(oi.UnitPrice)` with just `oi.UnitPrice` (line ~532)
3. Remove `sharedmodel` import if no longer used

- [ ] **Step 2: Update order.go**

In `internal/module/order/biz/order.go`:

1. Replace all DB→domain casts:
   - `sharedmodel.Concurrency(o.ProductCost)` → `o.ProductCost`
   - `sharedmodel.Concurrency(o.ProductDiscount)` → `o.ProductDiscount`
   - `sharedmodel.Concurrency(o.TransportCost)` → `o.TransportCost`
   - `sharedmodel.Concurrency(o.Total)` → `o.Total`
   - `sharedmodel.Concurrency(p.Amount)` → `p.Amount`
   - `sharedmodel.Concurrency(t.Cost)` → `t.Cost`
2. Remove `sharedmodel` import if no longer used

- [ ] **Step 3: Update refund.go**

In `internal/module/order/biz/refund.go`:

1. Replace `sharedmodel.Concurrency(refund.Amount)` with just `refund.Amount` (line ~372)
2. Remove `sharedmodel` import if no longer used

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/
git commit -m "replace Concurrency with int64 in order biz layer"
```

---

### Task 8: Update Providers

**Files:**
- Modify: `internal/provider/transport/ghtk/ghtk.go`
- Modify: `internal/provider/payment/payment.go`

- [ ] **Step 1: Update ghtk.go**

In `internal/provider/transport/ghtk/ghtk.go`:

1. Change `Cost` field type from `sharedmodel.Concurrency` to `int64` (line ~46)
2. Change `calculateShippingCost` return type from `sharedmodel.Concurrency` to `int64` (line ~318)
3. Replace `sharedmodel.Int64ToConcurrency(15000)` with `15000` (line ~319)
4. Replace `sharedmodel.Int64ToConcurrency(...)` with the raw expression (line ~323)
5. Replace `sharedmodel.FloatToConcurrency((baseCost + weightCost).Float64() * serviceMultiplier)` with `int64(float64(baseCost+weightCost) * serviceMultiplier)` (line ~340)
6. Remove `sharedmodel` import

- [ ] **Step 2: Update payment.go**

In `internal/provider/payment/payment.go`:

1. Replace all `sharedmodel.Concurrency` field types with `int64` (lines ~29, ~59, ~70)
2. Remove `sharedmodel` import

- [ ] **Step 3: Commit**

```bash
git add internal/provider/
git commit -m "replace Concurrency with int64 in transport and payment providers"
```

---

### Task 9: Clean Up Examples and Regenerate

**Files:**
- Modify or Delete: `example/marshal/main.go`

- [ ] **Step 1: Update or delete example/marshal/main.go**

If the example only demonstrates Concurrency marshaling, delete the file. Otherwise, update to use `int64`.

- [ ] **Step 2: Regenerate sqlc for all modules with new currency columns**

Run for each module that got a new column:

```bash
go run ./cmd/pgtempl/ -module catalog -skip-schema-prefix -single-file=generated_queries.sql
go run ./cmd/pgtempl/ -module order -skip-schema-prefix -single-file=generated_queries.sql
go run ./cmd/pgtempl/ -module promotion -skip-schema-prefix -single-file=generated_queries.sql
go run ./cmd/pgtempl/ -module account -skip-schema-prefix -single-file=generated_queries.sql
sqlc generate
```

- [ ] **Step 3: Regenerate Restate proxies if interfaces changed**

```bash
make generate
```

- [ ] **Step 4: Build and verify**

```bash
make build
```

Expected: Clean compilation with no errors.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "regenerate sqlc queries and restate proxies after currency refactor"
```

---

### Task 10: Run Migrations and Smoke Test

- [ ] **Step 1: Run database migrations**

```bash
make migrate
```

Expected: All migrations apply cleanly.

- [ ] **Step 2: Seed data (if applicable)**

```bash
make seed
```

- [ ] **Step 3: Start dev server and verify**

```bash
make dev
```

Expected: Server starts without errors. Test a product listing endpoint to verify prices are returned as minor unit integers (not 1e9-scaled).

- [ ] **Step 4: Final commit if any fixups needed**

```bash
git add .
git commit -m "fix post-migration issues"
```

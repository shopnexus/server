# Promotion Module

## Overview

The Promotion module is a self-contained domain module within the ShopNexus e-commerce backend. It is responsible for managing promotional campaigns, discount rules, scheduling, price calculation, and determining which products or categories a promotion applies to.

The module follows a layered architecture:

- **Database layer** -- PostgreSQL schema, migrations, SQLC-generated query code
- **Business logic layer** (`biz/`) -- Domain rules, validation, discount calculation, price orchestration
- **Transport layer** (`transport/echo/`) -- HTTP handlers using Echo v4
- **Model layer** (`model/`) -- Shared domain types exposed to other modules
- **Dependency injection** (`fx.go`) -- Uber fx wiring

All monetary values are stored as `int64` in the database and represented using the custom `Concurrency` type (an `int64` alias with a fixed-point precision of `1e9`) in Go code.

---
## Database Schema

All tables live under the `promotion` PostgreSQL schema.

### Enums

```sql
CREATE TYPE "promotion"."type" AS ENUM (
    'Discount',
    'ShipDiscount',
    'Bundle',
    'BuyXGetY',
    'Cashback'
);

CREATE TYPE "promotion"."ref_type" AS ENUM (
    'ProductSpu',
    'ProductSku',
    'Category',
    'Brand'
);
```

| Enum | Values | Purpose |
|------|--------|---------|
| `promotion.type` | `Discount`, `ShipDiscount`, `Bundle`, `BuyXGetY`, `Cashback` | Classifies the kind of promotion |
| `promotion.ref_type` | `ProductSpu`, `ProductSku`, `Category`, `Brand` | Specifies what entity a promotion targets |

### Tables

#### `promotion.promotion`

The core promotion entity. Every promotion type (discount, bundle, etc.) extends this base record.

```sql
CREATE TABLE IF NOT EXISTS "promotion"."promotion" (
    "id"           UUID          NOT NULL DEFAULT gen_random_uuid(),
    "code"         TEXT          NOT NULL,
    "owner_id"     UUID,
    "type"         promotion.type NOT NULL,
    "title"        TEXT          NOT NULL,
    "description"  TEXT,
    "is_active"    BOOLEAN       NOT NULL,
    "auto_apply"   BOOLEAN       NOT NULL,
    "date_started" TIMESTAMPTZ(3) NOT NULL,
    "date_ended"   TIMESTAMPTZ(3),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "promotion_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Nullable | Description |
|--------|------|----------|-------------|
| `id` | `UUID` | No | Auto-generated primary key |
| `code` | `TEXT` | No | Unique alphanumeric promotion code (3-50 chars) |
| `owner_id` | `UUID` | Yes | The account that created the promotion |
| `type` | `promotion.type` | No | Enum classifying the promotion variant |
| `title` | `TEXT` | No | Human-readable title (3-200 chars) |
| `description` | `TEXT` | Yes | Optional longer description (up to 1000 chars) |
| `is_active` | `BOOLEAN` | No | Whether the promotion is currently enabled |
| `auto_apply` | `BOOLEAN` | No | If true, the promotion is applied automatically without a code |
| `date_started` | `TIMESTAMPTZ(3)` | No | When the promotion becomes valid |
| `date_ended` | `TIMESTAMPTZ(3)` | Yes | When the promotion expires (NULL = no expiry) |
| `date_created` | `TIMESTAMPTZ(3)` | No | Row creation timestamp |
| `date_updated` | `TIMESTAMPTZ(3)` | No | Row last-update timestamp |

#### `promotion.ref`

Links a promotion to the entities (products, categories, brands) it applies to. A single promotion can have multiple refs.

```sql
CREATE TABLE IF NOT EXISTS "promotion"."ref" (
    "id"           BIGSERIAL NOT NULL,
    "promotion_id" UUID      NOT NULL,
    "ref_type"     promotion.ref_type NOT NULL,
    "ref_id"       UUID      NOT NULL,
    CONSTRAINT "ref_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Nullable | Description |
|--------|------|----------|-------------|
| `id` | `BIGSERIAL` | No | Auto-incrementing primary key |
| `promotion_id` | `UUID` | No | FK to `promotion.promotion(id)`, cascading delete/update |
| `ref_type` | `promotion.ref_type` | No | What kind of entity this ref points to |
| `ref_id` | `UUID` | No | The ID of the referenced entity |

#### `promotion.schedule`

Defines recurring activation windows for promotions using cron expressions.

```sql
CREATE TABLE IF NOT EXISTS "promotion"."schedule" (
    "id"            BIGSERIAL      NOT NULL,
    "promotion_id"  UUID           NOT NULL,
    "timezone"      TEXT           NOT NULL,
    "cron_rule"     TEXT           NOT NULL,
    "duration"      INTEGER        NOT NULL,
    "next_run_at"   TIMESTAMPTZ(3),
    "last_run_at"   TIMESTAMPTZ(3),
    CONSTRAINT "schedule_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Nullable | Description |
|--------|------|----------|-------------|
| `id` | `BIGSERIAL` | No | Auto-incrementing primary key |
| `promotion_id` | `UUID` | No | FK to `promotion.promotion(id)`, cascading delete/update |
| `timezone` | `TEXT` | No | IANA timezone string (e.g. `Asia/Ho_Chi_Minh`) |
| `cron_rule` | `TEXT` | No | Cron expression defining the recurrence pattern |
| `duration` | `INTEGER` | No | Duration of each activation window (in seconds) |
| `next_run_at` | `TIMESTAMPTZ(3)` | Yes | The next calculated activation time |
| `last_run_at` | `TIMESTAMPTZ(3)` | Yes | The last time this schedule was triggered |

#### `promotion.discount`

Type-specific data for promotions of type `Discount`. Shares the same primary key as the parent `promotion.promotion` row (1:1 relationship via FK on `id`).

```sql
CREATE TABLE IF NOT EXISTS "promotion"."discount" (
    "id"               UUID             NOT NULL,
    "min_spend"        BIGINT           NOT NULL,
    "max_discount"     BIGINT           NOT NULL,
    "discount_percent" DOUBLE PRECISION,
    "discount_price"   BIGINT,
    CONSTRAINT "discount_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Nullable | Description |
|--------|------|----------|-------------|
| `id` | `UUID` | No | PK and FK to `promotion.promotion(id)`, cascading delete/update |
| `min_spend` | `BIGINT` | No | Minimum order amount required to activate the discount |
| `max_discount` | `BIGINT` | No | Maximum discount amount that can be applied (cap) |
| `discount_percent` | `DOUBLE PRECISION` | Yes | Percentage discount (0.0 to 1.0); mutually exclusive with `discount_price` |
| `discount_price` | `BIGINT` | Yes | Fixed-amount discount; mutually exclusive with `discount_percent` |

**Important:** Exactly one of `discount_percent` or `discount_price` should be set (the other must be NULL). The business layer enforces this mutual exclusivity.

### Indexes

```sql
-- Unique code per promotion
CREATE UNIQUE INDEX "promotion_code_key"
    ON "promotion"."promotion" ("code");

-- Unique combination of promotion + ref_type + ref_id
CREATE UNIQUE INDEX "ref_promotion_id_ref_type_ref_id_key"
    ON "promotion"."ref" ("promotion_id", "ref_type", "ref_id");
```

### Foreign Key Constraints

| Constraint | Source | Target | On Delete | On Update |
|------------|--------|--------|-----------|-----------|
| `ref_promotion_id_fkey` | `ref.promotion_id` | `promotion.id` | CASCADE | CASCADE |
| `schedule_promotion_id_fkey` | `schedule.promotion_id` | `promotion.id` | CASCADE | CASCADE |
| `discount_id_fkey` | `discount.id` | `promotion.id` | CASCADE | CASCADE |

All child tables cascade on delete, so removing a promotion automatically cleans up its refs, schedules, and discount data.

---

## Promotion Types and Reference Types

### Promotion Types (`promotion.type`)

| Type | Description |
|------|-------------|
| `Discount` | A direct price discount (percentage-based or fixed-amount). Fully implemented with the `discount` sub-table. |
| `ShipDiscount` | A shipping cost discount. Schema-defined; business logic is marked as TODO. |
| `Bundle` | A bundle deal (e.g. buy items together for a reduced total). Schema-defined; not yet implemented. |
| `BuyXGetY` | Buy X items and get Y items free/discounted. Schema-defined; not yet implemented. |
| `Cashback` | Cash-back reward after purchase. Schema-defined; not yet implemented. |

### Reference Types (`promotion.ref_type`)

Promotions are scoped to specific products or categories via the `ref` table.

| Ref Type | Description | Applicability Check |
|----------|-------------|---------------------|
| `ProductSpu` | Targets a specific product SPU (Standard Product Unit) | Matches when the order item's SPU ID equals `ref_id` |
| `ProductSku` | Targets a specific product SKU (Stock Keeping Unit) | Matches when the order item's SKU ID equals `ref_id` |
| `Category` | Targets an entire product category | Matches when the order item's SPU category ID equals `ref_id` |
| `Brand` | Targets a brand | Schema-defined; not yet handled in applicability logic |

---

## Discount Calculation Logic

The discount calculation engine lives in `biz/price.go`. It is the core function used by the order/checkout flow.

### `CalculatePromotedPrices`

Entry point for computing discounted prices across a set of order line items.

**Algorithm:**

1. Collect all promotion codes submitted by the buyer.
2. Query all active promotions that are either `auto_apply = true` OR match one of the submitted codes.
3. Fetch all associated refs and discount records for those promotions.
4. For each SKU in the order:
   - Compute the base product cost: `unit_price * quantity`.
   - Iterate over every discount promotion, check if it is applicable to this SKU (via ref matching).
   - Calculate the discounted price for each applicable discount.
   - Keep the **best** (lowest) discounted price across all applicable discounts.
5. Return a map of `skuID -> OrderPrice` with the final `ProductCost` and `ShipCost`.

### `IsPromotionApplicable`

Determines if a promotion applies to a given SKU by checking the promotion's refs:

```go
func IsPromotionApplicable(promo Promotion, spu ProductSpu, skuID uuid.UUID) bool {
    for _, ref := range promo.Refs {
        switch ref.RefType {
        case "Category":
            return spu.Category.ID == ref.RefID
        case "ProductSpu":
            return ref.RefID == spu.ID
        case "ProductSku":
            return ref.RefID == skuID
        }
    }
    return false
}
```

### `CalculateDiscountedItemPrice`

Applies a single discount rule to a price:

```go
func CalculateDiscountedItemPrice(originalPrice Concurrency, discount PromotionDiscount) Concurrency
```

**Steps:**

1. If `originalPrice < minSpend`, return `originalPrice` unchanged (minimum spend not met).
2. If `discount_percent` is set: compute `discountAmount = originalPrice * discount_percent / 100`, then cap at `maxDiscount`.
3. If `discount_price` is set: use `min(discount_price, maxDiscount)` as the flat discount.
4. Return `max(originalPrice - discount, 0)` (the price never goes below zero).

### Mutual Exclusivity Rule

The business layer enforces that `discount_percent` and `discount_price` cannot both be set on a single discount record. When updating, setting one automatically NULLs the other:

```go
if params.DiscountPercent.Valid && params.DiscountPrice.Valid {
    return fmt.Errorf("either percentage or price discount can be set, not both")
}
```

---

## Scheduling System

The `promotion.schedule` table supports recurring promotion activation through cron-based scheduling.

Each schedule entry defines:

- **`cron_rule`**: A cron expression (e.g. `0 9 * * 1` for every Monday at 9 AM) that determines when the promotion window opens.
- **`timezone`**: The IANA timezone in which the cron expression is evaluated.
- **`duration`**: How long (in seconds) the promotion remains active after each trigger.
- **`next_run_at` / `last_run_at`**: Tracking fields for the scheduler to determine upcoming and past activations.

The SQLC queries support range filtering on `duration`, `next_run_at`, and `last_run_at`, as well as nullable update semantics for the timestamp fields. The schedule table is fully CRUDable.

**Note:** The cron evaluation/trigger logic (the actual scheduler daemon that reads `next_run_at` and activates/deactivates promotions) is not implemented within this module; only the data model and query layer are present.

---

## API Endpoints

All endpoints are registered under the `/api/v1/catalog/promotion` route group.

| Method | Path | Handler | Auth Required | Description |
|--------|------|---------|---------------|-------------|
| `GET` | `/api/v1/catalog/promotion/:id` | `GetPromotion` | No | Retrieve a single promotion by UUID |
| `GET` | `/api/v1/catalog/promotion` | `ListPromotion` | No | List promotions with pagination |
| `DELETE` | `/api/v1/catalog/promotion/:id` | `DeletePromotion` | Yes | Delete a promotion by UUID |
| `POST` | `/api/v1/catalog/promotion/discount` | `CreateDiscount` | Yes | Create a new discount promotion |
| `PATCH` | `/api/v1/catalog/promotion/discount` | `UpdateDiscount` | Yes | Update an existing discount promotion |

### Request / Response Details

#### GET `/api/v1/catalog/promotion/:id`

**Path params:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | UUID | Yes | Promotion ID |

**Response:** `PromotionPromotion` object.

#### GET `/api/v1/catalog/promotion`

**Query params:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `page` | int32 | No | Page number (must be > 0) |
| `limit` | int32 | No | Items per page (max 100, default varies) |
| `cursor` | string | No | Cursor for cursor-based pagination |

**Response:** Paginated list of `Promotion` objects including their associated refs. Uses `COUNT(*) OVER()` for total count.

#### DELETE `/api/v1/catalog/promotion/:id`

**Path params:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | UUID | Yes | Promotion ID |

**Authentication:** Required. Uses JWT claims from the request.

**Response:** Success message.

#### POST `/api/v1/catalog/promotion/discount`

**Request body (JSON):**

| Field | Type | Required | Validation | Description |
|-------|------|----------|------------|-------------|
| `code` | string | Yes | `alphanum,min=3,max=50` | Unique promotion code |
| `refs` | array | No | `dive` | Array of `{ref_type, ref_id}` objects |
| `title` | string | Yes | `min=3,max=200` | Promotion title |
| `description` | string | No | `max=1000` | Promotion description |
| `is_active` | bool | Yes | -- | Whether the promotion is active |
| `auto_apply` | bool | Yes | -- | Whether to auto-apply without code entry |
| `date_started` | datetime | Yes | -- | Start date (ISO 8601) |
| `date_ended` | datetime | No | Must be after `date_started` | End date |
| `min_spend` | int64 | Yes | `min=0,max=1000000000` | Minimum spend threshold |
| `max_discount` | int64 | Yes | `min=0,max=1000000000` | Maximum discount cap |
| `discount_percent` | float | No | `min=0,max=1` | Percentage discount (0.0 to 1.0) |
| `discount_price` | int64 | No | `min=1,max=1000000000` | Fixed-amount discount |

**Authentication:** Required.

**Response:** `PromotionDiscount` object.

#### PATCH `/api/v1/catalog/promotion/discount`

**Request body (JSON):**

| Field | Type | Required | Validation | Description |
|-------|------|----------|------------|-------------|
| `id` | UUID | Yes | -- | Promotion ID to update |
| `code` | string | No | -- | New promotion code |
| `owner_id` | UUID | No | -- | New owner |
| `refs` | array | No | `dive` | Replace all refs with this new set |
| `title` | string | No | -- | New title |
| `description` | string | No | -- | New description |
| `is_active` | bool | No | -- | Toggle active state |
| `date_started` | datetime | No | -- | New start date |
| `date_ended` | datetime | No | -- | New end date |
| `null_date_ended` | bool | No | -- | Set `date_ended` to NULL (remove expiry) |
| `min_spend` | int64 | No | `min=0,max=1000000000` | New minimum spend |
| `max_discount` | int64 | No | `min=0,max=1000000000` | New maximum discount |
| `discount_percent` | float | No | `min=0,max=1` | New percentage discount |
| `discount_price` | int64 | No | `min=1,max=1000000000` | New fixed-amount discount |

**Authentication:** Required.

**Note on ref updates:** When `refs` is provided, the update performs a delete-and-replace: all existing refs for the promotion are removed, then the new set is inserted. If `refs` is omitted (nil), existing refs are left untouched.

**Response:** `PromotionDiscount` object.

---

## Models and Types

### `promotionmodel.Promotion`

The primary domain model exposed to other modules.

```go
type Promotion struct {
    ID          uuid.UUID                 `json:"id"`
    Code        string                    `json:"code"`
    OwnerID     uuid.NullUUID             `json:"owner_id"`
    Type        promotiondb.PromotionType `json:"type"`
    Title       string                    `json:"title"`
    Description null.String               `json:"description"`
    IsActive    bool                      `json:"is_active"`
    AutoApply   bool                      `json:"auto_apply"`
    DateStarted time.Time                 `json:"date_started"`
    DateEnded   null.Time                 `json:"date_ended"`
    DateCreated time.Time                 `json:"date_created"`
    DateUpdated time.Time                 `json:"date_updated"`
    Refs        []PromotionRef            `json:"refs"`
}
```

### `promotionmodel.PromotionRef`

Associates a promotion with a target entity.

```go
type PromotionRef struct {
    RefType promotiondb.PromotionRefType `validate:"required,validateFn=Valid"`
    RefID   uuid.UUID                    `validate:"required"`
}
```

### `promotionmodel.PromotionDiscount`

Extends `Promotion` with discount-specific fields. Uses the `Concurrency` type for monetary values.

```go
type PromotionDiscount struct {
    Promotion
    MinSpend        sharedmodel.Concurrency     `json:"min_spend"`
    MaxDiscount     sharedmodel.Concurrency     `json:"max_discount"`
    DiscountPercent null.Float                  `json:"discount_percent"`
    DiscountPrice   sharedmodel.NullConcurrency `json:"discount_price"`
}
```

### Currency Type (`sharedmodel.Concurrency`)

Monetary values are stored as `int64` with a fixed-point precision of `1e9`. This avoids floating-point rounding issues while supporting sub-cent precision.

```go
type Concurrency int64

const FloatingPointPrecision = 1e9

func (c Concurrency) String() string {
    return strconv.FormatFloat(float64(c)/FloatingPointPrecision, 'f', -1, 64)
}
```

Arithmetic methods: `Add`, `Sub`, `Mul`, `Div`, `Float64`, `String`, `MarshalJSON`.

---

## Key Patterns
### Best-Price Selection

When multiple discounts are applicable to the same SKU, the system selects the one that produces the lowest final price for the customer. This is a "best deal for the buyer" strategy.

---

## Active Promotion Query

The `ListActivePromotion` query is the main entry point for the price calculation engine. It fetches all promotions that are currently valid:

```sql
SELECT *
FROM promotion.promotion
WHERE is_active = true
  AND (date_ended IS NULL OR date_ended > NOW())
  AND (
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL)
    AND (
      ("auto_apply" = sqlc.narg('auto_apply') OR sqlc.narg('auto_apply') IS NULL) OR
      "id" = ANY(sqlc.slice('id')) OR
      "code" = ANY(sqlc.slice('code'))
    )
  );
```

This query combines:
- **Auto-apply promotions** (applied to every order automatically)
- **Code-based promotions** (applied only when the buyer enters a specific code)
- **Type filtering** (e.g. only `Discount` type promotions)

The `OR` logic means a promotion is returned if it is auto-apply, OR if its ID or code is explicitly requested.

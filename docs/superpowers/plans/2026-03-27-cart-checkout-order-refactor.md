# Cart, Checkout & Order Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decouple checkout from order creation — checkout creates pending items, sellers confirm and group into orders, buyers pay confirmed orders with exact totals.

**Architecture:** Order items are created at checkout (orphaned, no order). Sellers see pending items grouped by buyer/address/SPU, combine them into orders with transport. Payment happens after confirmation. Shipment infrastructure is replaced by generic transport system.

**Tech Stack:** Go, PostgreSQL (SQLC + pgtempl), Restate SDK, Echo HTTP, Uber fx

**Spec:** `docs/superpowers/specs/2026-03-27-cart-checkout-order-refactor-design.md`

---

## File Map

### Database
- **Create:** `internal/module/order/db/migrations/0002_refactor_checkout_order.up.sql`
- **Create:** `internal/module/order/db/migrations/0002_refactor_checkout_order.down.sql`
- **Create:** `internal/module/order/db/queries/item_custom.sql` (pending item queries)
- **Modify:** `internal/module/order/db/queries/order.sql` (custom seller order queries)
- **Regenerate:** `internal/module/order/db/queries/order_order.sql` (pgtempl)
- **Regenerate:** `internal/module/order/db/queries/order_item.sql` (pgtempl)
- **Regenerate:** `internal/module/order/db/queries/order_refund.sql` (pgtempl — transport_id replaces shipment_id)
- **Regenerate:** `internal/module/order/db/queries/order_transport.sql` (pgtempl — new table)
- **Delete:** `internal/module/order/db/queries/order_shipment.sql` (replaced by transport)
- **Regenerate:** `internal/module/order/db/sqlc/` (sqlc generate)

### Infrastructure
- **Create:** `internal/infras/transport/transport.go` (Client interface + types)
- **Create:** `internal/infras/transport/ghtk/ghtk.go` (GHTK transport implementation)
- **Delete:** `internal/infras/shipment/` (replaced by transport)

### Inventory Module
- **Modify:** `internal/module/inventory/biz/interface.go` (add ReleaseInventory)
- **Create:** `internal/module/inventory/biz/release.go` (implementation)
- **Modify:** `internal/module/inventory/db/queries/inventory.sql` (add ReleaseInventory query)
- **Regenerate:** `internal/module/inventory/biz/restate_gen.go`

### Order Module — Models
- **Modify:** `internal/module/order/model/order.go` (rename fields, add PendingItem type)
- **Modify:** `internal/module/order/model/error.go` (new error sentinels)

### Order Module — Biz
- **Modify:** `internal/module/order/biz/interface.go` (new interface + struct)
- **Modify:** `internal/module/order/biz/checkout.go` (rewrite: create pending items only)
- **Create:** `internal/module/order/biz/incoming.go` (seller view + confirmation)
- **Create:** `internal/module/order/biz/pay.go` (buyer payment flow)
- **Modify:** `internal/module/order/biz/order.go` (adapt queries, hydration)
- **Modify:** `internal/module/order/biz/refund.go` (shipment → transport)
- **Rename:** `internal/module/order/biz/shipment_map.go` → `transport_map.go`
- **Delete:** `internal/module/order/biz/order_vendor.go` (replaced by incoming.go)
- **Regenerate:** `internal/module/order/biz/restate_gen.go`

### Order Module — Transport/Echo
- **Modify:** `internal/module/order/transport/echo/order.go` (new routes + handlers)
- **Create:** `internal/module/order/transport/echo/incoming.go` (seller endpoints)
- **Delete:** `internal/module/order/transport/echo/order_vendor.go` (replaced)

### Order Module — Wiring
- **Modify:** `internal/module/order/fx.go` (wire transport deps)

---

## Task 1: Database Migration

**Files:**
- Create: `internal/module/order/db/migrations/0002_refactor_checkout_order.up.sql`
- Create: `internal/module/order/db/migrations/0002_refactor_checkout_order.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- New item_status enum
CREATE TYPE "order"."item_status" AS ENUM ('Pending', 'Confirmed', 'Canceled');

-- Rename shipment_status → transport_status
ALTER TYPE "order"."shipment_status" RENAME TO "transport_status";

-- Create transport table (replaces shipment)
CREATE TABLE IF NOT EXISTS "order"."transport" (
    "id"           UUID NOT NULL DEFAULT gen_random_uuid(),
    "option"       TEXT NOT NULL,
    "status"       "order"."transport_status" NOT NULL DEFAULT 'Pending',
    "cost"         BIGINT NOT NULL DEFAULT 0,
    "data"         JSONB NOT NULL DEFAULT '{}',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "transport_pkey" PRIMARY KEY ("id")
);

-- Modify order.item: add new columns
ALTER TABLE "order"."item" ADD COLUMN "account_id" UUID;
ALTER TABLE "order"."item" ADD COLUMN "seller_id" UUID;
ALTER TABLE "order"."item" ADD COLUMN "address" TEXT NOT NULL DEFAULT '';
ALTER TABLE "order"."item" ADD COLUMN "status" "order"."item_status" NOT NULL DEFAULT 'Pending';
ALTER TABLE "order"."item" ADD COLUMN "paid_amount" BIGINT NOT NULL DEFAULT 0;
ALTER TABLE "order"."item" ADD COLUMN "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE "order"."item" ADD COLUMN "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- Backfill existing items from their orders
UPDATE "order"."item" i
SET
    "account_id" = o."customer_id",
    "seller_id" = o."vendor_id",
    "address" = o."address",
    "status" = 'Confirmed',
    "paid_amount" = i."unit_price" * i."quantity",
    "date_created" = o."date_created"
FROM "order"."order" o
WHERE i."order_id" = o."id";

-- Make account_id and seller_id NOT NULL after backfill
-- (only if all existing items have orders — they should)
ALTER TABLE "order"."item" ALTER COLUMN "account_id" SET NOT NULL;
ALTER TABLE "order"."item" ALTER COLUMN "seller_id" SET NOT NULL;

-- Make order_id nullable
ALTER TABLE "order"."item" ALTER COLUMN "order_id" DROP NOT NULL;

-- Drop old unique constraint, add new indexes
ALTER TABLE "order"."item" DROP CONSTRAINT IF EXISTS "item_order_id_sku_id_key";
CREATE INDEX item_pending_seller_idx ON "order"."item" ("seller_id", "status") WHERE "order_id" IS NULL;
CREATE INDEX item_pending_account_idx ON "order"."item" ("account_id", "status") WHERE "order_id" IS NULL;

-- Modify order.order: rename columns
ALTER TABLE "order"."order" RENAME COLUMN "customer_id" TO "buyer_id";
ALTER TABLE "order"."order" RENAME COLUMN "vendor_id" TO "seller_id";
ALTER TABLE "order"."order" RENAME COLUMN "ship_cost" TO "transport_cost";

-- Drop ship_discount
ALTER TABLE "order"."order" DROP COLUMN IF EXISTS "ship_discount";

-- Make payment_id nullable
ALTER TABLE "order"."order" ALTER COLUMN "payment_id" DROP NOT NULL;

-- Handle shipment → transport FK on order
ALTER TABLE "order"."order" DROP CONSTRAINT IF EXISTS "order_shipment_id_fkey";
ALTER TABLE "order"."order" RENAME COLUMN "shipment_id" TO "transport_id";
ALTER TABLE "order"."order" ADD CONSTRAINT "order_transport_id_fkey"
    FOREIGN KEY ("transport_id") REFERENCES "order"."transport" ("id") ON DELETE SET NULL;

-- Handle shipment → transport FK on refund
ALTER TABLE "order"."refund" DROP CONSTRAINT IF EXISTS "refund_shipment_id_fkey";
ALTER TABLE "order"."refund" RENAME COLUMN "shipment_id" TO "transport_id";
ALTER TABLE "order"."refund" ADD CONSTRAINT "refund_transport_id_fkey"
    FOREIGN KEY ("transport_id") REFERENCES "order"."transport" ("id") ON DELETE SET NULL;

-- Rename vendor index on order
DROP INDEX IF EXISTS "order"."order_vendor_id_idx";
CREATE INDEX "order_seller_id_idx" ON "order"."order" ("seller_id");

-- Rename refund shipment index
DROP INDEX IF EXISTS "order"."refund_shipment_id_idx";
CREATE INDEX "refund_transport_id_idx" ON "order"."refund" ("transport_id");

-- Update payment FK to allow SET NULL on delete (orders can exist without payment)
ALTER TABLE "order"."order" DROP CONSTRAINT IF EXISTS "order_payment_id_fkey";
ALTER TABLE "order"."order" ADD CONSTRAINT "order_payment_id_fkey"
    FOREIGN KEY ("payment_id") REFERENCES "order"."payment" ("id") ON DELETE SET NULL ON UPDATE CASCADE;

-- Migrate shipment data to transport
INSERT INTO "order"."transport" ("id", "option", "status", "cost", "data", "date_created")
SELECT "id", "option", "status"::text::"order"."transport_status", "cost",
       jsonb_build_object(
           'tracking_code', "tracking_code",
           'label_url', "label_url",
           'new_cost', "new_cost",
           'date_eta', "date_eta",
           'from_address', "from_address",
           'to_address', "to_address",
           'weight_grams', "weight_grams",
           'length_cm', "length_cm",
           'width_cm', "width_cm",
           'height_cm', "height_cm"
       ),
       "date_created"
FROM "order"."shipment";

-- Drop shipment table
DROP TABLE IF EXISTS "order"."shipment" CASCADE;
```

- [ ] **Step 2: Write the down migration**

Reverse all changes: recreate shipment, rename columns back, drop transport, etc.

- [ ] **Step 3: Run migration**

Run: `make migrate`
Expected: Migration applies cleanly.

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/db/migrations/0002_*
git commit -m "add migration for checkout/order refactor"
```

---

## Task 2: Regenerate SQLC Queries

**Files:**
- Regenerate: `internal/module/order/db/queries/order_order.sql`
- Regenerate: `internal/module/order/db/queries/order_item.sql`
- Delete: `internal/module/order/db/queries/order_shipment.sql`
- Create: `internal/module/order/db/queries/item_custom.sql`
- Modify: `internal/module/order/db/queries/order.sql`
- Regenerate: `internal/module/order/db/sqlc/`

- [ ] **Step 1: Delete shipment query file**

```bash
rm internal/module/order/db/queries/order_shipment.sql
```

- [ ] **Step 2: Run pgtempl to regenerate queries**

```bash
go run ./cmd/pgtempl/ -module order -skip-schema-prefix
```

This regenerates all query files from the migration DDL:
- `order_order.sql` — renamed columns: buyer_id, seller_id, transport_id, transport_cost; nullable payment_id
- `order_item.sql` — new columns: account_id, seller_id, address, status, paid_amount, date_created, date_updated; nullable order_id
- `order_refund.sql` — transport_id replaces shipment_id
- `order_transport.sql` — new table (replaces order_shipment.sql)

- [ ] **Step 3: Manually fix generated queries**

pgtempl doesn't handle DROP COLUMN. Verify `order_order.sql` no longer references `ship_discount`. Check that `order_item.sql` reflects nullable `order_id`.

- [ ] **Step 4: Write custom pending item queries**

Create `internal/module/order/db/queries/item_custom.sql`:

```sql
-- Custom item queries

-- name: ListPendingItemsBySeller :many
SELECT *
FROM "order"."item"
WHERE "seller_id" = @seller_id AND "status" = 'Pending'
ORDER BY "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CountPendingItemsBySeller :one
SELECT COUNT(*)
FROM "order"."item"
WHERE "seller_id" = @seller_id AND "status" = 'Pending';

-- name: ListPendingItemsByAccount :many
SELECT *
FROM "order"."item"
WHERE "account_id" = @account_id AND "status" = ANY(@status::order.item_status[])
ORDER BY "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CountPendingItemsByAccount :one
SELECT COUNT(*)
FROM "order"."item"
WHERE "account_id" = @account_id AND "status" = ANY(@status::order.item_status[]);

-- name: ConfirmItems :exec
UPDATE "order"."item"
SET "order_id" = @order_id, "status" = 'Confirmed', "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = ANY(@ids::bigint[]) AND "status" = 'Pending';

-- name: CancelItem :exec
UPDATE "order"."item"
SET "status" = 'Canceled', "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = @id AND "account_id" = @account_id AND "status" = 'Pending';

-- name: CancelItemsBySeller :exec
UPDATE "order"."item"
SET "status" = 'Canceled', "date_updated" = CURRENT_TIMESTAMP
WHERE "id" = ANY(@ids::bigint[]) AND "seller_id" = @seller_id AND "status" = 'Pending';

-- name: CancelItemsByOrder :exec
UPDATE "order"."item"
SET "status" = 'Canceled', "date_updated" = CURRENT_TIMESTAMP
WHERE "order_id" = @order_id;

-- name: CreatePendingItem :one
INSERT INTO "order"."item" (
    "account_id", "seller_id", "address", "status",
    "sku_id", "sku_name", "quantity", "unit_price", "paid_amount",
    "note", "serial_ids"
)
VALUES ($1, $2, $3, 'Pending', $4, $5, $6, $7, $8, $9, $10)
RETURNING *;
```

- [ ] **Step 5: Update custom order query**

Update `internal/module/order/db/queries/order.sql`:

```sql
-- Custom order queries

-- name: ListCountSellerOrder :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() as total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment" p ON embed_order."payment_id" = p."id"
WHERE embed_order."seller_id" = @seller_id
    AND (p."status" = ANY(sqlc.slice('payment_status')) OR sqlc.slice('payment_status') IS NULL)
    AND (embed_order."status" = ANY(sqlc.slice('order_status')) OR sqlc.slice('order_status') IS NULL)
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: SetOrderPayment :exec
UPDATE "order"."order"
SET "payment_id" = @payment_id
WHERE "id" = ANY(@ids::uuid[]) AND "buyer_id" = @buyer_id AND "payment_id" IS NULL;
```

- [ ] **Step 6: Run sqlc generate**

```bash
sqlc generate
```

Expected: Generates updated Go types in `internal/module/order/db/sqlc/`.

- [ ] **Step 7: Verify build**

```bash
go build ./internal/module/order/db/sqlc/...
```

- [ ] **Step 8: Commit**

```bash
git add internal/module/order/db/
git commit -m "regenerate order queries for checkout/order refactor"
```

---

## Task 3: Transport Infrastructure

**Files:**
- Create: `internal/infras/transport/transport.go`
- Create: `internal/infras/transport/ghtk/ghtk.go`
- Delete: `internal/infras/shipment/` (after transport is wired)

- [ ] **Step 1: Create transport client interface**

Create `internal/infras/transport/transport.go`:

```go
package transport

import (
    "context"
    "encoding/json"

    "github.com/google/uuid"
    sharedmodel "shopnexus-server/internal/shared/model"
)

type Client interface {
    Config() sharedmodel.OptionConfig
    Quote(ctx context.Context, params QuoteParams) (QuoteResult, error)
    Create(ctx context.Context, params CreateParams) (Transport, error)
    Track(ctx context.Context, id string) (TrackResult, error)
    Cancel(ctx context.Context, id string) error
}

type QuoteParams struct {
    Items       []ItemMetadata
    FromAddress string
    ToAddress   string
}

type ItemMetadata struct {
    SkuID          uuid.UUID
    Quantity       int64
    PackageDetails json.RawMessage
}

type CreateParams struct {
    Items       []ItemMetadata
    FromAddress string
    ToAddress   string
    Option      string
}

type QuoteResult struct {
    Cost int64
    Data json.RawMessage
}

type Transport struct {
    ID     uuid.UUID
    Option string
    Cost   int64
    Data   json.RawMessage
}

type TrackResult struct {
    Status string
    Data   json.RawMessage
}
```

- [ ] **Step 2: Create GHTK transport implementation**

Create `internal/infras/transport/ghtk/ghtk.go` — adapt from existing `internal/infras/shipment/ghtk/ghtk.go`:
- Implement `transport.Client` interface
- Move shipping cost calculation logic
- Store tracking/label data in `json.RawMessage` instead of dedicated fields
- Keep the 3 variants (Express, Standard, Economy)

- [ ] **Step 3: Verify build**

```bash
go build ./internal/infras/transport/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/infras/transport/
git commit -m "add generic transport infrastructure replacing shipment"
```

---

## Task 4: Inventory ReleaseInventory

**Files:**
- Modify: `internal/module/inventory/biz/interface.go`
- Create: `internal/module/inventory/biz/release.go`
- Modify: `internal/module/inventory/db/queries/inventory.sql`
- Regenerate: `internal/module/inventory/biz/restate_gen.go`

- [ ] **Step 1: Add ReleaseInventory SQL query**

Add to `internal/module/inventory/db/queries/inventory.sql`:

```sql
-- name: ReleaseInventory :exec
UPDATE "inventory"."stock"
SET "stock" = "stock" + @amount, "taken" = "taken" - @amount
WHERE "ref_id" = @ref_id AND "ref_type" = @ref_type AND "taken" >= @amount;
```

- [ ] **Step 2: Run sqlc generate**

```bash
sqlc generate
```

- [ ] **Step 3: Add ReleaseInventory to interface**

Add to `internal/module/inventory/biz/interface.go` InventoryBiz interface:

```go
ReleaseInventory(ctx context.Context, params ReleaseInventoryParams) error
```

- [ ] **Step 4: Create release.go implementation**

Create `internal/module/inventory/biz/release.go`:

```go
package inventorybiz

import (
    restate "github.com/restatedev/sdk-go"
    inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
    sharedmodel "shopnexus-server/internal/shared/model"
)

type ReleaseInventoryParams struct {
    Items []ReleaseInventoryItem
}

type ReleaseInventoryItem struct {
    RefType inventorydb.InventoryStockRefType
    RefID   uuid.UUID
    Amount  int64
}

func (b *InventoryHandler) ReleaseInventory(ctx restate.Context, params ReleaseInventoryParams) error {
    _, err := restate.Run(ctx, func(ctx restate.RunContext) (any, error) {
        for _, item := range params.Items {
            if err := b.storage.Querier().ReleaseInventory(ctx, inventorydb.ReleaseInventoryParams{
                RefID:   item.RefID,
                RefType: item.RefType,
                Amount:  item.Amount,
            }); err != nil {
                return nil, sharedmodel.WrapErr("release inventory", err)
            }
            // If serial_required, update serials back to Active
            // TODO: need serial IDs to release specific serials
        }
        return nil, nil
    })
    return err
}
```

- [ ] **Step 5: Regenerate restate client**

```bash
go generate ./internal/module/inventory/biz/
```

- [ ] **Step 6: Verify build**

```bash
go build ./internal/module/inventory/...
```

- [ ] **Step 7: Commit**

```bash
git add internal/module/inventory/
git commit -m "add ReleaseInventory to inventory module"
```

---

## Task 5: Order Models

**Files:**
- Modify: `internal/module/order/model/order.go`
- Modify: `internal/module/order/model/error.go`

- [ ] **Step 1: Update order.go models**

Rewrite `internal/module/order/model/order.go`:

```go
package ordermodel

import (
    "encoding/json"
    "time"

    "github.com/google/uuid"
    "github.com/guregu/null/v6"
    commonmodel "shopnexus-server/internal/module/common/model"
    orderdb "shopnexus-server/internal/module/order/db/sqlc"
    sharedmodel "shopnexus-server/internal/shared/model"
)

type Order struct {
    ID              uuid.UUID               `json:"id"`
    BuyerID         uuid.UUID               `json:"buyer_id"`
    SellerID        uuid.UUID               `json:"seller_id"`
    TransportID     uuid.UUID               `json:"transport_id"`
    Payment         *Payment                `json:"payment"`
    Status          orderdb.OrderStatus     `json:"status"`
    Address         string                  `json:"address"`
    ProductCost     sharedmodel.Concurrency `json:"product_cost"`
    ProductDiscount sharedmodel.Concurrency `json:"product_discount"`
    TransportCost   sharedmodel.Concurrency `json:"transport_cost"`
    Total           sharedmodel.Concurrency `json:"total"`
    Note            null.String             `json:"note"`
    Data            json.RawMessage         `json:"data"`
    DateCreated     time.Time               `json:"date_created"`
    Items           []OrderItem             `json:"items"`
}

type OrderItem struct {
    ID        int64                  `json:"id"`
    OrderID   *uuid.UUID             `json:"order_id"`
    AccountID uuid.UUID              `json:"account_id"`
    SellerID  uuid.UUID              `json:"seller_id"`
    Address   string                 `json:"address"`
    Status    orderdb.OrderItemStatus `json:"status"`
    SkuID     uuid.UUID              `json:"sku_id"`
    SkuName   string                 `json:"sku_name"`
    Quantity  int64                  `json:"quantity"`
    UnitPrice int64                  `json:"unit_price"`
    PaidAmount int64                 `json:"paid_amount"`
    Note      null.String            `json:"note"`
    SerialIds json.RawMessage        `json:"serial_ids"`
    DateCreated time.Time            `json:"date_created"`
    Resources []commonmodel.Resource `json:"resources"`
}

type Payment struct {
    ID          int64                   `json:"id"`
    AccountID   uuid.UUID               `json:"account_id"`
    Option      string                  `json:"option"`
    Status      orderdb.OrderStatus     `json:"status"`
    Amount      sharedmodel.Concurrency `json:"amount"`
    Data        json.RawMessage         `json:"data"`
    DateCreated time.Time               `json:"date_created"`
    DatePaid    null.Time               `json:"date_paid"`
    DateExpired time.Time               `json:"date_expired"`
}

type Refund struct {
    ID            uuid.UUID                 `json:"id"`
    AccountID     uuid.UUID                 `json:"account_id"`
    OrderID       uuid.UUID                 `json:"order_id"`
    ConfirmedByID uuid.NullUUID             `json:"confirmed_by_id"`
    TransportID   uuid.NullUUID             `json:"transport_id"`
    Method        orderdb.OrderRefundMethod `json:"method"`
    Status        orderdb.OrderStatus       `json:"status"`
    Reason        string                    `json:"reason"`
    Address       null.String               `json:"address"`
    DateCreated   time.Time                 `json:"date_created"`
    Resources     []commonmodel.Resource    `json:"resources"`
}
```

- [ ] **Step 2: Update error.go**

Add new error sentinels to `internal/module/order/model/error.go`:

```go
var (
    ErrItemsNotSameBuyer    = sharedmodel.NewError(400, "all items must belong to the same buyer").Terminal()
    ErrItemsNotSameAddress  = sharedmodel.NewError(400, "all items must have the same address").Terminal()
    ErrItemNotPending       = sharedmodel.NewError(400, "item is not in pending status").Terminal()
    ErrItemNotOwnedBySeller = sharedmodel.NewError(403, "item does not belong to this seller").Terminal()
    ErrOrderNotPayable      = sharedmodel.NewError(400, "order is not payable").Terminal()
    ErrOrderAlreadyPaid     = sharedmodel.NewError(400, "order is already paid").Terminal()
    ErrUnknownTransportOption = sharedmodel.NewError(400, "unknown transport option").Terminal()
)
```

Remove obsolete errors: `ErrMissingShippingQuote`, `ErrUnknownShipmentOption`, `ErrShipmentCannotCancel`.

- [ ] **Step 3: Verify build**

```bash
go build ./internal/module/order/model/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/model/
git commit -m "update order models for checkout/order refactor"
```

---

## Task 6: Order Biz Interface

**Files:**
- Modify: `internal/module/order/biz/interface.go`

- [ ] **Step 1: Rewrite interface.go**

```go
package orderbiz

import (
    "context"

    "github.com/google/uuid"
    ordermodel "shopnexus-server/internal/module/order/model"
    sharedmodel "shopnexus-server/internal/shared/model"
)

//go:generate go run shopnexus-server/cmd/genrestate -interface OrderBiz -service Order
type OrderBiz interface {
    // Checkout
    Checkout(ctx context.Context, params CheckoutParams) (CheckoutResult, error)

    // Pending Items (buyer)
    ListPendingItems(ctx context.Context, params ListPendingItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
    CancelPendingItem(ctx context.Context, params CancelPendingItemParams) error

    // Incoming Items (seller)
    ListIncomingItems(ctx context.Context, params ListIncomingItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
    ConfirmItems(ctx context.Context, params ConfirmItemsParams) (ordermodel.Order, error)
    RejectItems(ctx context.Context, params RejectItemsParams) error

    // Orders
    GetOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
    ListOrders(ctx context.Context, params ListOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
    ListSellerOrders(ctx context.Context, params ListSellerOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
    CancelOrder(ctx context.Context, params CancelOrderParams) error

    // Payment
    PayOrders(ctx context.Context, params PayOrdersParams) (PayOrdersResult, error)
    VerifyPayment(ctx context.Context, params VerifyPaymentParams) error

    // Cart (unchanged)
    GetCart(ctx context.Context, params GetCartParams) ([]ordermodel.CartItem, error)
    UpdateCart(ctx context.Context, params UpdateCartParams) error
    ClearCart(ctx context.Context, params ClearCartParams) error

    // Refund (unchanged interface)
    ListRefunds(ctx context.Context, params ListRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error)
    CreateRefund(ctx context.Context, params CreateRefundParams) (ordermodel.Refund, error)
    UpdateRefund(ctx context.Context, params UpdateRefundParams) (ordermodel.Refund, error)
    CancelRefund(ctx context.Context, params CancelRefundParams) error
    ConfirmRefund(ctx context.Context, params ConfirmRefundParams) (ordermodel.Refund, error)
}
```

Update `OrderHandler` struct:

```go
type OrderHandler struct {
    storage      OrderStorage
    paymentMap   map[string]payment.Client
    transportMap map[string]transport.Client  // was shipmentMap
    account      accountbiz.AccountBiz
    catalog      catalogbiz.CatalogBiz
    inventory    inventorybiz.InventoryBiz
    promotion    promotionbiz.PromotionBiz
    common       commonbiz.CommonBiz
}
```

Update constructor `NewOrderHandler` to accept `transport` instead of shipment deps.

- [ ] **Step 2: Define param structs**

In the same file or a separate `params.go`, define all new param structs:

```go
type CheckoutParams struct {
    Account accountmodel.AuthenticatedAccount
    BuyNow  bool           `validate:"omitempty"`
    Items   []CheckoutItem `validate:"required,min=1,dive"`
}

type CheckoutItem struct {
    SkuID    uuid.UUID `json:"sku_id" validate:"required"`
    Quantity int64     `json:"quantity" validate:"required,gt=0"`
    Address  string    `json:"address" validate:"required,min=5,max=500"`
    Note     string    `json:"note" validate:"max=500"`
}

type CheckoutResult struct {
    Items []ordermodel.OrderItem `json:"items"`
}

type ListPendingItemsParams struct {
    AccountID uuid.UUID
    Status    []orderdb.OrderItemStatus
    Limit     *int32
    Offset    *int32
}

type CancelPendingItemParams struct {
    AccountID uuid.UUID
    ItemID    int64
}

type ListIncomingItemsParams struct {
    SellerID uuid.UUID
    Limit    *int32
    Offset   *int32
}

type ConfirmItemsParams struct {
    Account         accountmodel.AuthenticatedAccount
    ItemIDs         []int64 `json:"item_ids" validate:"required,min=1"`
    TransportOption string  `json:"transport_option" validate:"required"`
    Note            string  `json:"note" validate:"max=500"`
}

type RejectItemsParams struct {
    Account accountmodel.AuthenticatedAccount
    ItemIDs []int64 `json:"item_ids" validate:"required,min=1"`
}

type ListSellerOrdersParams struct {
    SellerID      uuid.UUID
    PaymentStatus []orderdb.OrderStatus
    OrderStatus   []orderdb.OrderStatus
    Limit         *int32
    Offset        *int32
}

type PayOrdersParams struct {
    Account       accountmodel.AuthenticatedAccount
    OrderIDs      []uuid.UUID `json:"order_ids" validate:"required,min=1"`
    PaymentOption string      `json:"payment_option" validate:"required"`
}

type PayOrdersResult struct {
    Payment     ordermodel.Payment `json:"payment"`
    RedirectUrl *string             `json:"url"`
}
```

- [ ] **Step 3: Verify build** (will have errors from missing implementations — that's expected)

```bash
go vet ./internal/module/order/biz/
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/interface.go
git commit -m "rewrite order biz interface for checkout/order refactor"
```

---

## Task 7: Checkout Flow (Rewrite)

**Files:**
- Modify: `internal/module/order/biz/checkout.go`

- [ ] **Step 1: Rewrite checkout.go**

New checkout only creates pending items — no payment, no order, no transport:

```go
func (b *OrderHandler) Checkout(ctx restate.Context, params CheckoutParams) (CheckoutResult, error) {
    // 1. Validate
    if err := validate(params); err != nil { return zero, err }
    if params.BuyNow && len(params.Items) != 1 {
        return zero, ordermodel.ErrBuyNowSingleSkuOnly
    }

    // 2. Fetch SKU details from catalog (price, name, SPU seller_id)
    skuIDs := lo.Map(params.Items, func(item CheckoutItem, _ int) uuid.UUID { return item.SkuID })
    skus, err := b.catalog.ListProductSku(ctx, catalogbiz.ListProductSkuParams{ID: skuIDs})
    spuIDs := lo.Map(skus.Data, func(sku catalogdb.CatalogProductSku, _ int) uuid.UUID { return sku.SpuID })
    spus, err := b.catalog.ListProductSpu(ctx, catalogbiz.ListProductSpuParams{ID: spuIDs})
    // Build skuID→seller map from SPU.AccountID

    // 3. Reserve inventory
    inventories, err := b.inventory.ReserveInventory(ctx, inventorybiz.ReserveInventoryParams{
        Items: lo.Map(params.Items, func(item CheckoutItem, _ int) inventorybiz.ReserveInventoryItem {
            return inventorybiz.ReserveInventoryItem{
                RefType: inventorydb.InventoryStockRefTypeProductSku,
                RefID:   item.SkuID,
                Amount:  item.Quantity,
            }
        }),
    })

    // 4. Create pending items (in durable step)
    items, err := restate.Run(ctx, func(ctx restate.RunContext) ([]int64, error) {
        var itemIDs []int64
        for i, checkoutItem := range params.Items {
            sku := skuMap[checkoutItem.SkuID]
            sellerID := spuSellerMap[sku.SpuID]
            serialIDs := inventories[i].SerialIDs

            item, err := b.storage.Querier().CreatePendingItem(ctx, orderdb.CreatePendingItemParams{
                AccountID:  params.Account.ID,
                SellerID:   sellerID,
                Address:    checkoutItem.Address,
                SkuID:      checkoutItem.SkuID,
                SkuName:    sku.Name, // or built from attributes
                Quantity:   checkoutItem.Quantity,
                UnitPrice:  sku.Price,
                PaidAmount: sku.Price * checkoutItem.Quantity, // TODO: promotions
                Note:       null.NewString(checkoutItem.Note, checkoutItem.Note != ""),
                SerialIds:  marshalSerialIDs(serialIDs),
            })
            if err != nil { return nil, sharedmodel.WrapErr("create pending item", err) }
            itemIDs = append(itemIDs, item.ID)
        }
        return itemIDs, nil
    })

    // 5. Remove from cart (skip if BuyNow)
    if !params.BuyNow {
        restate.RunVoid(ctx, func(ctx restate.RunContext) error {
            for _, item := range params.Items {
                b.storage.Querier().RemoveCheckoutItem(ctx, orderdb.RemoveCheckoutItemParams{
                    AccountID: params.Account.ID,
                    SkuID:     item.SkuID,
                })
            }
            return nil
        })
    }

    // 6. Track analytics
    restate.ServiceSend(ctx, "Analytic", "CreateInteraction").Send(...)

    // 7. Return created items
    // Fetch full items by IDs and return
    return CheckoutResult{Items: createdItems}, nil
}
```

- [ ] **Step 2: Remove old checkout-related code**

Delete `ListCheckoutCart` method (no longer needed — checkout doesn't need a preview step since there's no payment/shipment calculation).

- [ ] **Step 3: Verify build**

```bash
go build ./internal/module/order/biz/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/checkout.go
git commit -m "rewrite checkout to create pending items only"
```

---

## Task 8: Seller Incoming + Confirmation

**Files:**
- Create: `internal/module/order/biz/incoming.go`
- Delete: `internal/module/order/biz/order_vendor.go`

- [ ] **Step 1: Create incoming.go**

```go
package orderbiz

// ListIncomingItems — seller sees pending items grouped by buyer → SPU
func (b *OrderHandler) ListIncomingItems(ctx restate.Context, params ListIncomingItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
    // Query pending items by seller_id
    // Hydrate with SKU resources
    // Return paginated
}

// ConfirmItems — seller groups pending items into an order
func (b *OrderHandler) ConfirmItems(ctx restate.Context, params ConfirmItemsParams) (ordermodel.Order, error) {
    // 1. Fetch items by IDs, validate all Pending + same seller
    // 2. Validate same account_id (buyer) and same address
    // 3. Get transport client, create transport (quote + create)
    // 4. Calculate costs:
    //    productCost = sum(unit_price * quantity)
    //    productDiscount = productCost - sum(paid_amount)
    //    transportCost = transport.cost
    //    total = productCost - productDiscount + transportCost
    // 5. Create order (payment_id = NULL)
    // 6. Update items: set order_id, status = Confirmed
    // 7. Notify buyer via fire-and-forget
    // 8. Return order
}

// RejectItems — seller rejects pending items
func (b *OrderHandler) RejectItems(ctx restate.Context, params RejectItemsParams) error {
    // 1. Validate items are Pending + belong to seller
    // 2. Release inventory for each item
    // 3. Update status → Canceled
    // 4. Notify buyer
}
```

- [ ] **Step 2: Delete order_vendor.go**

```bash
rm internal/module/order/biz/order_vendor.go
```

- [ ] **Step 3: Verify build**

```bash
go build ./internal/module/order/biz/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/incoming.go
git rm internal/module/order/biz/order_vendor.go
git commit -m "add seller incoming view and confirmation flow"
```

---

## Task 9: Payment Flow

**Files:**
- Create: `internal/module/order/biz/pay.go`

- [ ] **Step 1: Create pay.go**

```go
package orderbiz

// PayOrders — buyer pays one or more confirmed orders
func (b *OrderHandler) PayOrders(ctx restate.Context, params PayOrdersParams) (PayOrdersResult, error) {
    // 1. Validate payment option exists
    // 2. Fetch orders, validate: all belong to buyer, status=Pending, payment_id=NULL
    // 3. Calculate total amount across all orders
    // 4. Create payment record (durable step)
    // 5. Call payment provider (VNPay → redirect URL, COD → auto-success)
    // 6. Set payment_id on all orders (durable step)
    // 7. Return payment + redirect URL
}
```

- [ ] **Step 2: Update VerifyPayment**

Adapt `VerifyPayment` in `order.go` to work with new schema (payment updates order status on success).

- [ ] **Step 3: Verify build**

```bash
go build ./internal/module/order/biz/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/pay.go
git commit -m "add buyer payment flow for confirmed orders"
```

---

## Task 10: Cancel Flow

**Files:**
- Modify: `internal/module/order/biz/checkout.go` (add CancelPendingItem)
- Modify: `internal/module/order/biz/order.go` (update CancelOrder)

- [ ] **Step 1: Implement CancelPendingItem**

```go
func (b *OrderHandler) CancelPendingItem(ctx restate.Context, params CancelPendingItemParams) error {
    // 1. Fetch item, validate Pending + belongs to account
    // 2. Release inventory
    // 3. Update status → Canceled
}
```

- [ ] **Step 2: Update CancelOrder**

Adapt existing `CancelOrder` for new schema:
- Check order status (must be Pending = unpaid, or paid but not fulfilled)
- Release inventory for all items
- Set items → Canceled
- If paid, initiate refund process

- [ ] **Step 3: Verify build**

```bash
go build ./internal/module/order/biz/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/
git commit -m "add cancel flows for pending items and orders"
```

---

## Task 11: Order Queries and Hydration

**Files:**
- Modify: `internal/module/order/biz/order.go`

- [ ] **Step 1: Update ListOrders and GetOrder**

Adapt for renamed columns (buyer_id, seller_id, transport_id, transport_cost). Update hydration to handle nullable payment_id (payment may be NULL for unpaid orders).

- [ ] **Step 2: Update hydrateOrders**

- Fetch items by order IDs (same logic, new column names)
- Fetch payment only if payment_id is not NULL
- Attach resources via common module

- [ ] **Step 3: Remove QuoteOrder**

Delete QuoteOrder method (transport cost determined at confirmation, not checkout).

- [ ] **Step 4: Verify build**

```bash
go build ./internal/module/order/biz/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/module/order/biz/order.go
git commit -m "update order queries and hydration for new schema"
```

---

## Task 12: Transport Map Setup

**Files:**
- Rename: `internal/module/order/biz/shipment_map.go` → `transport_map.go`

- [ ] **Step 1: Rename file**

```bash
git mv internal/module/order/biz/shipment_map.go internal/module/order/biz/transport_map.go
```

- [ ] **Step 2: Rewrite transport_map.go**

Replace `SetupShipmentMap` with `SetupTransportMap`:
- Initialize GHTK transport clients (Express, Standard, Economy)
- Store in `b.transportMap`
- Register option configs in common module (background goroutine)
- Add `getTransportClient(option string)` helper

- [ ] **Step 3: Verify build**

```bash
go build ./internal/module/order/biz/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/biz/transport_map.go
git commit -m "replace shipment map with transport map"
```

---

## Task 13: Refund Adaptation

**Files:**
- Modify: `internal/module/order/biz/refund.go`

- [ ] **Step 1: Update refund.go**

- Rename `ShipmentID` → `TransportID` in all refund methods
- Update DB queries to use transport instead of shipment
- Update notifications to use seller terminology

- [ ] **Step 2: Verify build**

```bash
go build ./internal/module/order/biz/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/module/order/biz/refund.go
git commit -m "update refund to use transport instead of shipment"
```

---

## Task 14: HTTP Handlers

**Files:**
- Modify: `internal/module/order/transport/echo/order.go`
- Create: `internal/module/order/transport/echo/incoming.go`
- Delete: `internal/module/order/transport/echo/order_vendor.go`

- [ ] **Step 1: Rewrite order.go routes**

Update `NewHandler` to register new routes:

```go
func NewHandler(e *echo.Echo, biz orderbiz.OrderBiz) *Handler {
    h := &Handler{biz: biz}
    g := e.Group("/api/v1/order", middleware.Auth)

    // Cart (unchanged)
    g.GET("/cart", h.GetCart)
    g.POST("/cart", h.UpdateCart)
    g.DELETE("/cart", h.ClearCart)

    // Checkout
    g.POST("/checkout", h.Checkout)
    g.GET("/checkout/items", h.ListPendingItems)
    g.DELETE("/checkout/items/:id", h.CancelPendingItem)

    // Incoming (seller)
    g.GET("/incoming", h.ListIncomingItems)
    g.POST("/incoming/confirm", h.ConfirmItems)
    g.POST("/incoming/reject", h.RejectItems)

    // Orders (literal paths before parameterized to avoid conflicts)
    g.GET("", h.ListOrders)
    g.GET("/seller", h.ListSellerOrders)
    g.GET("/:id", h.GetOrder)

    // Payment
    g.POST("/pay", h.PayOrders)

    // IPN (no auth)
    e.GET("/api/v1/order/ipn", h.VnpayVerifyIPN)

    // Refund (unchanged routes)
    g.GET("/refund", h.ListRefunds)
    g.POST("/refund", h.CreateRefund)
    g.PATCH("/refund", h.UpdateRefund)
    g.DELETE("/refund", h.CancelRefund)
    g.POST("/refund/confirm", h.ConfirmRefund)

    return h
}
```

- [ ] **Step 2: Update checkout handler**

Update `CheckoutRequest` struct:

```go
type CheckoutRequest struct {
    BuyNow bool                  `json:"buy_now"`
    Items  []CheckoutItemRequest `json:"items" validate:"required,min=1,dive"`
}

type CheckoutItemRequest struct {
    SkuID    uuid.UUID `json:"sku_id" validate:"required"`
    Quantity int64     `json:"quantity" validate:"required,gt=0"`
    Address  string    `json:"address" validate:"required,min=5,max=500"`
    Note     string    `json:"note" validate:"max=500"`
}
```

Remove: `ListCheckoutCart` handler, `QuoteOrder` handler, old `ConfirmOrder` handler.

- [ ] **Step 3: Create incoming.go handlers**

Create `internal/module/order/transport/echo/incoming.go`:

```go
package orderecho

// ListIncomingItems, ConfirmItems, RejectItems handlers
// ConfirmItemsRequest { ItemIDs []int64, TransportOption string, Note string }
// RejectItemsRequest { ItemIDs []int64 }
```

- [ ] **Step 4: Add PayOrders handler**

```go
type PayOrdersRequest struct {
    OrderIDs      []uuid.UUID `json:"order_ids" validate:"required,min=1"`
    PaymentOption string      `json:"payment_option" validate:"required"`
}
```

- [ ] **Step 5: Delete order_vendor.go**

```bash
rm internal/module/order/transport/echo/order_vendor.go
```

- [ ] **Step 6: Verify build**

```bash
go build ./internal/module/order/transport/...
```

- [ ] **Step 7: Commit**

```bash
git add internal/module/order/transport/echo/
git commit -m "update HTTP handlers for checkout/order refactor"
```

---

## Task 15: FX Wiring and Restate Regeneration

**Files:**
- Modify: `internal/module/order/fx.go`
- Regenerate: `internal/module/order/biz/restate_gen.go`

- [ ] **Step 1: Regenerate restate client**

```bash
go generate ./internal/module/order/biz/
```

- [ ] **Step 2: Update fx.go if needed**

Ensure constructor and provider signatures match the updated `NewOrderHandler`.

- [ ] **Step 3: Delete old shipment infrastructure**

```bash
rm -rf internal/infras/shipment/
```

Update any imports across the codebase that reference `internal/infras/shipment`.

- [ ] **Step 4: Full build**

```bash
go build ./...
```

Fix any remaining compilation errors.

- [ ] **Step 5: Run make generate**

```bash
make generate
```

Ensures all generated code is up to date.

- [ ] **Step 6: Commit**

```bash
git add .
git commit -m "wire fx, regenerate restate clients, remove old shipment"
```

---

## Task 16: Integration Verification

- [ ] **Step 1: Start infrastructure**

```bash
docker compose -f deployment/docker-compose.yml up -d
```

- [ ] **Step 2: Run migrations**

```bash
make migrate
```

- [ ] **Step 3: Start dev server**

```bash
make dev
```

Expected: Server starts without errors, Restate registration succeeds.

- [ ] **Step 4: Register services**

```bash
make register
```

- [ ] **Step 5: Smoke test key flows**

Test via curl or API client:
1. Add to cart → verify cart works
2. Checkout → verify pending items created, inventory reserved
3. List incoming (as seller) → verify items visible
4. Confirm items (as seller) → verify order created with transport
5. Pay order (as buyer) → verify payment flow
6. Cancel pending item → verify inventory released

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "complete cart/checkout/order refactor"
```

# Order Module

## Overview

The Order module is a core domain module of the ShopNexus e-commerce platform. It manages the full lifecycle of customer orders, from shopping cart management through checkout, payment processing, shipment booking, and post-sale refund/dispute resolution.

**Tech stack:** Go, PostgreSQL (via pgx), SQLC (code-generated queries), Echo v4 (HTTP transport), Uber fx (dependency injection), Watermill + Kafka (event-driven messaging).

**Key responsibilities:**

- Shopping cart management (add, update, remove, clear items)
- Checkout orchestration (inventory reservation, shipment creation, payment initiation, order creation)
- Order quoting (pre-checkout price estimation with promotions and shipping)
- Payment processing via pluggable providers (VNPay QR/Bank/ATM, Cash on Delivery)
- Shipment booking via pluggable providers (GHTK Express/Standard/Economy)
- Order confirmation by vendors (with optional shipment detail updates)
- Order cancellation
- Refund requests (PickUp / DropOff methods)
- Refund dispute resolution
- Event publishing for order lifecycle events (order.created, order.paid, order.confirmed)
- Analytics interaction tracking (add_to_cart, purchase, cancel_order, refund_requested)

**Cross-module dependencies:**

| Module      | Usage                                                      |
|-------------|------------------------------------------------------------|
| `account`   | Authenticated account identity, vendor default contacts    |
| `catalog`   | Product SPU/SKU lookup, pricing, package details           |
| `inventory` | Inventory reservation during checkout                      |
| `promotion` | Promotion code validation and price calculation            |
| `common`    | Resource management (refund images), service option configs|
| `analytic`  | User interaction tracking for recommendations              |

---

## Database Schema

All tables reside in the PostgreSQL schema `"order"`.

### Enums

```sql
CREATE TYPE "order"."status" AS ENUM (
    'Pending', 'Processing', 'Success', 'Canceled', 'Failed'
);

CREATE TYPE "order"."shipment_status" AS ENUM (
    'Pending', 'LabelCreated', 'InTransit', 'OutForDelivery',
    'Delivered', 'Failed', 'Cancelled'
);

CREATE TYPE "order"."refund_method" AS ENUM (
    'PickUp', 'DropOff'
);
```

### Tables

#### `order.cart_item`

Stores items in a customer's shopping cart. Each account can have at most one entry per SKU (enforced by unique index).

```sql
CREATE TABLE IF NOT EXISTS "order"."cart_item" (
    "id"         BIGSERIAL NOT NULL,
    "account_id" UUID      NOT NULL,
    "sku_id"     UUID      NOT NULL,
    "quantity"   BIGINT    NOT NULL,
    CONSTRAINT "cart_item_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "cart_item_account_id_sku_id_key"
    ON "order"."cart_item" ("account_id", "sku_id");
```

#### `order.payment`

Tracks payment records. One payment can be shared across multiple orders (one-to-many relationship).

```sql
CREATE TABLE IF NOT EXISTS "order"."payment" (
    "id"           BIGSERIAL       NOT NULL,
    "account_id"   UUID            NOT NULL,
    "option"       TEXT            NOT NULL,       -- e.g. "vnpay_qr", "system-cod"
    "status"       "order"."status" NOT NULL DEFAULT 'Pending',
    "amount"       BIGINT          NOT NULL,       -- total amount in smallest currency unit
    "data"         JSONB           NOT NULL,       -- provider-specific metadata
    "date_created" TIMESTAMPTZ(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_paid"    TIMESTAMPTZ(3),                 -- NULL until payment is confirmed
    "date_expired" TIMESTAMPTZ(3)  NOT NULL,       -- payment expiry deadline
    CONSTRAINT "payment_pkey" PRIMARY KEY ("id")
);
```

#### `order.shipment`

Stores shipment details for each order, including package dimensions, costs, tracking, and ETA.

```sql
CREATE TABLE IF NOT EXISTS "order"."shipment" (
    "id"            UUID                     NOT NULL DEFAULT gen_random_uuid(),
    "option"        TEXT                     NOT NULL,   -- e.g. "ghtk_express"
    "tracking_code" TEXT,                                -- third-party tracking ID
    "status"        "order"."shipment_status" NOT NULL DEFAULT 'Pending',
    "label_url"     TEXT,
    "cost"          BIGINT                   NOT NULL,   -- original quoted cost
    "new_cost"      BIGINT                   NOT NULL,   -- recalculated cost after confirmation
    "date_eta"      TIMESTAMPTZ(3)           NOT NULL,
    "from_address"  TEXT                     NOT NULL,
    "to_address"    TEXT                     NOT NULL,
    "weight_grams"  INTEGER                  NOT NULL,
    "length_cm"     INTEGER                  NOT NULL,
    "width_cm"      INTEGER                  NOT NULL,
    "height_cm"     INTEGER                  NOT NULL,
    "date_created"  TIMESTAMPTZ(3)           NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "shipment_pkey" PRIMARY KEY ("id")
);
```

#### `order.order`

The central order table. Each checkout item becomes a separate order, all linked to the same payment.

```sql
CREATE TABLE IF NOT EXISTS "order"."order" (
    "id"               UUID            NOT NULL DEFAULT gen_random_uuid(),
    "customer_id"      UUID            NOT NULL,
    "vendor_id"        UUID            NOT NULL,
    "payment_id"       BIGINT          NOT NULL,
    "shipment_id"      UUID            NOT NULL,
    "confirmed_by_id"  UUID,                        -- vendor who confirmed the order
    "status"           "order"."status" NOT NULL DEFAULT 'Pending',
    "address"          TEXT            NOT NULL,
    "product_cost"     BIGINT          NOT NULL,
    "product_discount" BIGINT          NOT NULL,
    "ship_cost"        BIGINT          NOT NULL,
    "ship_discount"    BIGINT          NOT NULL,
    "total"            BIGINT          NOT NULL,
    "note"             TEXT,
    "data"             JSONB           NOT NULL,
    "date_created"     TIMESTAMPTZ(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "order_pkey" PRIMARY KEY ("id")
);
```

**Foreign keys:**

- `order.payment_id` -> `order.payment(id)` ON DELETE NO ACTION ON UPDATE CASCADE
- `order.shipment_id` -> `order.shipment(id)` ON DELETE SET NULL ON UPDATE CASCADE

#### `order.item`

Line items within an order. Each order item tracks a specific SKU, quantity, unit price, and optionally assigned serial IDs from inventory.

```sql
CREATE TABLE IF NOT EXISTS "order"."item" (
    "id"         BIGSERIAL       NOT NULL,
    "order_id"   UUID            NOT NULL,
    "sku_id"     UUID            NOT NULL,
    "sku_name"   TEXT            NOT NULL,
    "quantity"   BIGINT          NOT NULL,
    "unit_price" BIGINT          NOT NULL,
    "note"       TEXT,
    "serial_ids" JSONB,                          -- JSON array of assigned serial IDs
    CONSTRAINT "item_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "item_order_id_idx" ON "order"."item" ("order_id");
CREATE INDEX IF NOT EXISTS "item_sku_id_idx" ON "order"."item" ("sku_id");
CREATE UNIQUE INDEX IF NOT EXISTS "item_order_id_sku_id_key"
    ON "order"."item" ("order_id", "sku_id");
```

**Foreign key:** `item.order_id` -> `order.order(id)` ON DELETE CASCADE ON UPDATE CASCADE

#### `order.refund`

Refund requests linked to an order. Supports two return methods: PickUp (courier collects from customer) and DropOff (customer ships back).

```sql
CREATE TABLE IF NOT EXISTS "order"."refund" (
    "id"              UUID                    NOT NULL DEFAULT gen_random_uuid(),
    "account_id"      UUID                    NOT NULL,
    "order_id"        UUID                    NOT NULL,
    "confirmed_by_id" UUID,                             -- vendor who confirmed/rejected
    "shipment_id"     UUID,                             -- return shipment, if applicable
    "method"          "order"."refund_method" NOT NULL,
    "status"          "order"."status"        NOT NULL DEFAULT 'Pending',
    "reason"          TEXT                    NOT NULL,
    "address"         TEXT,                              -- required for PickUp method
    "date_created"    TIMESTAMPTZ(3)          NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "refund_pkey" PRIMARY KEY ("id")
);
```

**Foreign keys:**

- `refund.order_id` -> `order.order(id)` ON DELETE NO ACTION ON UPDATE CASCADE
- `refund.shipment_id` -> `order.shipment(id)` ON DELETE NO ACTION ON UPDATE CASCADE

#### `order.refund_dispute`

Disputes raised against refund decisions. Either the customer or vendor can issue a dispute.

```sql
CREATE TABLE IF NOT EXISTS "order"."refund_dispute" (
    "id"           UUID            NOT NULL DEFAULT gen_random_uuid(),
    "refund_id"    UUID            NOT NULL,
    "issued_by_id" UUID            NOT NULL,
    "reason"       TEXT            NOT NULL,
    "status"       "order"."status" NOT NULL DEFAULT 'Pending',
    "date_created" TIMESTAMPTZ(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "refund_dispute_pkey" PRIMARY KEY ("id")
);
```

**Foreign key:** `refund_dispute.refund_id` -> `order.refund(id)` ON DELETE NO ACTION ON UPDATE CASCADE

### Indexes Summary

| Index Name                          | Table            | Columns                   | Type   |
|-------------------------------------|------------------|---------------------------|--------|
| `cart_item_account_id_sku_id_key`   | `cart_item`      | `account_id, sku_id`      | UNIQUE |
| `order_vendor_id_idx`               | `order`          | `vendor_id`               | INDEX  |
| `item_order_id_idx`                 | `item`           | `order_id`                | INDEX  |
| `item_sku_id_idx`                   | `item`           | `sku_id`                  | INDEX  |
| `item_order_id_sku_id_key`          | `item`           | `order_id, sku_id`        | UNIQUE |
| `refund_account_id_idx`             | `refund`         | `account_id`              | INDEX  |
| `refund_order_id_idx`               | `refund`         | `order_id`                | INDEX  |
| `refund_confirmed_by_id_idx`        | `refund`         | `confirmed_by_id`         | INDEX  |
| `refund_shipment_id_idx`            | `refund`         | `shipment_id`             | INDEX  |
| `refund_dispute_refund_id_idx`      | `refund_dispute` | `refund_id`               | INDEX  |
| `refund_dispute_issued_by_id_idx`   | `refund_dispute` | `issued_by_id`            | INDEX  |

---

## API Endpoints

All endpoints are registered under the `/api/v1/order` route group.

### Order Endpoints

| Method | Path                          | Handler              | Auth | Description                                         |
|--------|-------------------------------|----------------------|------|-----------------------------------------------------|
| GET    | `/api/v1/order`               | `ListOrders`         | Yes  | List orders with pagination                         |
| GET    | `/api/v1/order/:id`           | `GetOrder`           | Yes  | Get a single order by UUID                          |
| POST   | `/api/v1/order/checkout`      | `Checkout`           | Yes  | Perform checkout, create orders and payment          |
| POST   | `/api/v1/order/quote`         | `QuoteOrder`         | Yes  | Get price quote for items before checkout            |
| POST   | `/api/v1/order/confirm`       | `ConfirmOrder`       | Yes  | Vendor confirms a paid order                         |
| GET    | `/api/v1/order/vendor`        | `ListVendorOrder`    | Yes  | List orders belonging to the authenticated vendor    |
| GET    | `/api/v1/order/ipn`           | `VnpayVerifyIPN`     | No   | VNPay IPN callback endpoint for payment verification |

### Cart Endpoints

| Method | Path                              | Handler              | Auth | Description                                      |
|--------|-----------------------------------|----------------------|------|--------------------------------------------------|
| GET    | `/api/v1/order/cart`              | `GetCart`            | Yes  | Get all cart items for the authenticated user     |
| POST   | `/api/v1/order/cart`              | `UpdateCart`         | Yes  | Add/update/remove a cart item (by quantity or delta)|
| DELETE | `/api/v1/order/cart`              | `ClearCart`          | Yes  | Remove all items from the cart                    |
| GET    | `/api/v1/order/cart-checkout`     | `ListCheckoutCart`   | Yes  | Preview checkout items (supports Buy Now mode)    |

### Refund Endpoints

| Method | Path                              | Handler              | Auth | Description                                      |
|--------|-----------------------------------|----------------------|------|--------------------------------------------------|
| GET    | `/api/v1/order/refund`            | `ListRefunds`        | Yes  | List refund requests with pagination              |
| POST   | `/api/v1/order/refund`            | `CreateRefund`       | Yes  | Create a new refund request for an order          |
| PATCH  | `/api/v1/order/refund`            | `UpdateRefund`       | Yes  | Update an existing refund (reason, method, etc.)  |
| DELETE | `/api/v1/order/refund`            | `CancelRefund`       | Yes  | Cancel a pending refund request                   |
| POST   | `/api/v1/order/refund/confirm`    | `ConfirmRefund`      | Yes  | Vendor confirms/approves a refund request         |

---

## Business Logic

### Cart Management (`biz/cart.go`)

The cart system is an upsert-based model. Each (account_id, sku_id) pair is unique.

- **GetCart** -- Fetches all cart items for an account, hydrates each item with full SKU details from the catalog module and the first product resource (image) from the common module.
- **UpdateCart** -- Supports two modes:
  - `quantity`: Sets the cart item to an absolute quantity. If `quantity <= 0`, the item is deleted.
  - `delta_quantity`: Adds a delta to the current quantity (useful for increment/decrement buttons).
  - Uses an upsert SQL pattern (UPDATE ... WHERE ... / INSERT ... WHERE NOT EXISTS).
- **ClearCart** -- Deletes all cart items for the authenticated account.
- **ListCheckoutCart** -- Prepares items for the checkout preview page. Supports two modes:
  - **Regular checkout**: Filters cart items by the provided `sku_ids`.
  - **Buy Now**: Bypasses the cart entirely; takes a single `buy_now_sku_id` and `buy_now_quantity`.

All cart mutations track analytics interactions (`EventAddToCart`, `EventRemoveFromCart`).

### Checkout Flow (`biz/checkout.go`)

The checkout is a multi-step transactional operation:

```
1. Validate inputs
2. Fetch SKU and SPU details from catalog
3. Fetch vendor default contacts (for shipment origin addresses)
4. BEGIN TRANSACTION
   a. Remove checkout items from cart (skip if Buy Now)
   b. Reserve inventory (assigns serial IDs to order items)
   c. Create shipments via shipment provider (one per checkout item)
   d. Calculate promoted prices via promotion module
   e. Create a single payment record for the total amount
   f. Call payment provider to create payment (get redirect URL)
   g. Create order records (one per checkout item)
   h. Create order item records (combined or split by serial IDs based on sku.CanCombine)
5. COMMIT TRANSACTION
6. Fetch and return created orders
7. Track purchase analytics interactions
```

**Key design decisions:**

- Each checkout item becomes a separate `order.order` record, but all share the same `order.payment` record. This supports multi-vendor marketplaces where a single payment covers orders from different vendors.
- Inventory reservation happens within the same database transaction (cross-schema), ensuring atomicity.
- If a SKU has `CanCombine = true`, all reserved serial IDs are stored in a single order item. Otherwise, each serial ID gets its own order item row with `quantity = 1`.
- Payment expiry is configurable via `config.App.Order.PaymentExpiryDays` (defaults to 30 days).

### Order Quoting (`biz/order.go` -- `QuoteOrder`)

Provides a price estimate before checkout, including:

- Per-item shipping cost quotes from the shipment provider
- Promotion-applied prices from the promotion module
- Aggregated `product_cost`, `ship_cost`, and `total`

This allows the frontend to display a price breakdown without committing to a purchase.

### Order Cancellation (`biz/checkout.go` -- `CancelOrder`)

Cancels an order only when all three conditions are met:

1. Payment status is `Pending`
2. Shipment status is `Pending`
3. Order status is `Pending`

Within a transaction, it sets all three statuses to `Canceled`/`Cancelled` and tracks `EventCancelOrder` analytics.

### Order Confirmation by Vendor (`biz/order_vendor.go`)

Vendors confirm paid orders via `ConfirmOrder`:

1. Validates the order is in a confirmable state (payment = `Success`, order = `Pending`)
2. Within a transaction:
   - Updates order status to `Processing` and sets `confirmed_by_id`
   - Optionally updates shipment details (new `from_address`, package dimensions)
   - Creates shipment with the shipment provider if details changed
   - Updates shipment record with tracking code, label URL, new cost, and ETA
3. Publishes `order.confirmed` event

### Order Hydration (`biz/order.go` -- `hydrateOrders`)

When listing or fetching orders, the `hydrateOrders` helper:

1. Batch-fetches all order items by order IDs
2. Batch-fetches all payments by payment IDs
3. Assembles full `ordermodel.Order` structs with embedded payment and items

This avoids N+1 queries by using batch lookups with `ListItem` and `ListPayment`.

### Payment Verification (`biz/order.go` -- `VerifyPayment`)

Handles incoming payment provider callbacks (e.g., VNPay IPN):

1. Looks up the payment gateway client by ID
2. Calls `VerifyPayment` on the gateway (validates hash/signature)
3. Extracts the reference ID (payment ID)
4. Publishes an `order.paid` event to trigger downstream processing

---

## Payment System

### Interface (`internal/infras/payment/payment.go`)

```go
type Client interface {
    CreateOrder(ctx context.Context, params CreateOrderParams) (CreateOrderResult, error)
    VerifyPayment(ctx context.Context, data map[string]any) (VerifyResult, error)
}
```

The payment system uses a **pluggable provider pattern**. Providers are registered at startup in `SetupPaymentMap()` and stored in a `map[string]payment.Client` keyed by option ID.

### Providers

#### COD (Cash on Delivery) -- `internal/infras/payment/cod/cod.go`

- Option ID: `system-cod`
- Provider: `system`
- Method: `cod`
- `CreateOrder` returns an empty redirect URL (no online payment needed)
- `VerifyPayment` expects a `ref_id` in the data map (verified upon physical delivery)

#### VNPay -- `internal/infras/payment/vnpay/vnpay.go`

Creates three client instances, one per payment method:

| Option ID    | Method |
|-------------|--------|
| `vnpay_qr`  | QR     |
| `vnpay_bank`| Bank   |
| `vnpay_atm` | ATM    |

- `CreateOrder` builds a VNPay payment URL with HMAC-SHA512 signature and returns it as `RedirectURL`
- `VerifyPayment` validates the `vnp_SecureHash` from IPN callback data, extracts `vnp_TxnRef` as the reference ID
- Configuration: `TmnCode`, `HashSecret`, `ReturnURL` from app config

### Payment Setup Flow

During `OrderBiz` initialization, `SetupPaymentMap()`:

1. Creates COD and VNPay client instances
2. Registers all option configs with the common module's service options (so the frontend can discover available payment methods)

---

## Shipment System

### Interface (`internal/infras/shipment/shipment.go`)

```go
type Client interface {
    Config() sharedmodel.OptionConfig
    Quote(ctx context.Context, params CreateParams) (QuoteResult, error)
    Create(ctx context.Context, params CreateParams) (ShippingOrder, error)
    Track(ctx context.Context, id string) (TrackResult, error)
    Cancel(ctx context.Context, id string) error
}
```

`CreateParams` includes `FromAddress`, `ToAddress`, and `PackageDetails` (weight, length, width, height).

### Provider: GHTK (Giao Hang Tiet Kiem) -- `internal/infras/shipment/ghtk/ghtk.go`

Creates three client instances per service tier:

| Option ID        | Method    | ETA    | Cost Multiplier |
|-----------------|-----------|--------|-----------------|
| `ghtk_express`  | express   | 1 day  | 1.5x            |
| `ghtk_standard` | standard  | 2 days | 1.0x            |
| `ghtk_economy`  | economy   | 3 days | 0.8x            |

**Note:** The current implementation is a mock/fake implementation that calculates costs based on weight and service tier, and simulates shipment status progression over time. It stores shipments in-memory and generates fake tracking IDs in the format `GTK<HEX>`.

Cost formula: `(base_cost + weight_surcharge) * service_multiplier`, where base cost is 15,000 VND and the weight surcharge is 2,000 VND per additional kg above 1 kg.

### Shipment Status Lifecycle

```
Pending -> LabelCreated -> InTransit -> OutForDelivery -> Delivered
                                                      -> Failed
         -> Cancelled
```

---

## Refund and Dispute Flow

### Refund Lifecycle (`biz/refund.go`)

**Creating a refund:**

1. Validate inputs; if method is `PickUp`, an address is required
2. Within a transaction:
   - Fetch the order to validate it exists
   - Create a `refund` record with status `Pending`
   - Attach resource IDs (e.g., photos of damaged items) via the common module
3. Track `EventRefundReq` analytics for each SKU in the order

**Updating a refund:**

- Only refunds in `Pending` status can be updated
- Supports changing method, reason, address, and associated resource IDs
- If the method changes to `DropOff`, the address field is set to NULL

**Canceling a refund:**

- Sets the refund status to `Canceled` within a transaction

**Confirming a refund (by vendor):**

- Sets status to `Processing` and records the confirming vendor's ID
- Future: will trigger the shipment provider to schedule a pickup (for PickUp method)

### Refund Dispute

Disputes are tracked in the `order.refund_dispute` table. Each dispute:

- References a `refund_id`
- Records the `issued_by_id` (customer or vendor)
- Has its own `status` lifecycle (`Pending` -> `Processing` -> `Success`/`Canceled`/`Failed`)
- Includes a `reason` text field
- Tracks `date_created` and `date_updated` timestamps

SQLC queries support full CRUD operations on disputes with flexible filtering.

---

## Event Publishing (Pubsub)

The module uses a pubsub system with two backend implementations:

- **Kafka** (`internal/infras/pubsub/kafka.go`) -- Production backend using Watermill + Sarama. Supports consumer groups, offset management, and reliable message delivery.
- **In-memory** (`internal/infras/pubsub/mem.go`) -- Development/testing backend with goroutine-based fan-out.

### Topics

| Topic Constant         | Topic String       | Published When                          | Handler                |
|------------------------|--------------------|------------------------------------------|------------------------|
| `TopicOrderCreated`    | `order.created`    | (Subscribed, handler is a no-op stub)    | `OrderCreated`         |
| `TopicOrderPaid`       | `order.paid`       | After payment verification succeeds      | `OrderPaid`            |
| `TopicOrderConfirmed`  | `order.confirmed`  | After vendor confirms an order           | (Published only)       |

### Event Handlers (`biz/order_pubsub.go`)

- **OrderPaid**: When a payment is verified, this handler fetches the order by ID, then updates the associated payment status to `Success`.

### Setup

`SetupPubsub()` is called during `OrderBiz` initialization and subscribes handlers using `pubsub.DecodeWrap` for type-safe message decoding:

```go
b.pubsub.Subscribe(ordermodel.TopicOrderCreated, pubsub.DecodeWrap(b.OrderCreated))
b.pubsub.Subscribe(ordermodel.TopicOrderPaid, pubsub.DecodeWrap(b.OrderPaid))
```

The pubsub client is scoped to the `"order"` consumer group via `pubsub.Group("order")`.

---

## Models and Types

### Domain Models (`model/`)

**`ordermodel.CartItem`** -- Hydrated cart item combining SKU details, quantity, and first product image:

```go
type CartItem struct {
    SpuID    uuid.UUID
    Sku      catalogmodel.ProductSku
    Quantity int64
    Resource *commonmodel.Resource
}
```

**`ordermodel.Order`** -- Full order representation with embedded payment and items:

```go
type Order struct {
    ID              uuid.UUID
    CustomerID      uuid.UUID
    VendorID        uuid.UUID
    ShipmentID      uuid.UUID
    Payment         Payment
    Status          orderdb.OrderStatus
    Address         string
    ProductCost     sharedmodel.Concurrency
    ShipCost        sharedmodel.Concurrency
    ProductDiscount sharedmodel.Concurrency
    ShipDiscount    sharedmodel.Concurrency
    Total           sharedmodel.Concurrency
    Note            null.String
    Data            json.RawMessage
    DateCreated     time.Time
    Items           []orderdb.OrderItem
}
```

**`ordermodel.Payment`** -- Payment details embedded within orders:

```go
type Payment struct {
    ID          int64
    AccountID   uuid.UUID
    Option      string
    Status      orderdb.OrderStatus
    Amount      sharedmodel.Concurrency
    Data        json.RawMessage
    DateCreated time.Time
    DatePaid    null.Time
    DateExpired time.Time
}
```

**`ordermodel.Refund`** -- Refund request with attached resources:

```go
type Refund struct {
    ID            uuid.UUID
    AccountID     uuid.UUID
    OrderID       uuid.UUID
    ConfirmedByID uuid.NullUUID
    ShipmentID    uuid.NullUUID
    Method        orderdb.OrderRefundMethod
    Status        orderdb.OrderStatus
    Reason        string
    Address       null.String
    DateCreated   time.Time
    Resources     []commonmodel.Resource
}
```

### SQLC-Generated Models (`db/sqlc/models.go`)

SQLC generates Go structs that map directly to database rows:

- `OrderCartItem`, `OrderItem`, `OrderOrder`, `OrderPayment`, `OrderShipment`, `OrderRefund`, `OrderRefundDispute`
- Enum types: `OrderStatus`, `OrderShipmentStatus`, `OrderRefundMethod` (each with `Valid()`, `Scan()`, `Value()` methods and `Null*` wrapper types)

### Error Types (`model/error.go`)

```go
var (
    ErrOrderItemNotFound      = sharedmodel.NewError("order.order_item_not_found", ...)
    ErrPaymentGatewayNotFound = sharedmodel.NewError("order.payment_gateway_not_found", ...)
    ErrRefundAddressRequired  = sharedmodel.NewError("order.refund_address_required", ...)
    ErrRefundCannotBeUpdated  = sharedmodel.NewError("order.refund_cannot_be_updated", ...)
)
```

---

## Key Patterns

### Pluggable Provider Maps

Payment and shipment providers are registered in `map[string]Client` at startup. The checkout flow selects the appropriate client by the user-specified option string. This pattern allows adding new providers without modifying the business logic:

```go
paymentMap  map[string]payment.Client  // keyed by option ID, e.g. "vnpay_qr"
shipmentMap map[string]shipment.Client // keyed by option ID, e.g. "ghtk_express"
```

### Transactional Storage with `pgsqlc.Storage`

The module uses a generic transactional storage wrapper (`pgsqlc.Storage[*orderdb.Queries]`). Business methods accept an optional `Storage` parameter to allow callers to pass in an existing transaction. The `WithTx` method handles begin/commit/rollback automatically:

```go
b.storage.WithTx(ctx, params.Storage, func(txStorage OrderStorage) error {
    // All queries within this closure run in the same transaction
    txStorage.Querier().CreateOrder(ctx, ...)
    txStorage.Querier().CreateCopyItem(ctx, ...)
    return nil
})
```

Cross-module transactional work (e.g., inventory reservation during checkout) is achieved by creating a new `pgsqlc.Storage` for the target module using the transaction's connection:

```go
inventoryStorage := pgsqlc.NewStorage(txStorage.Conn(), inventorydb.New(txStorage.Conn()))
```
### Analytics Tracking

Cart and order operations fire analytics events via `b.analytic.TrackInteraction(...)`:

| Event                | Triggered By         |
|----------------------|----------------------|
| `EventAddToCart`     | `UpdateCart`         |
| `EventRemoveFromCart`| `UpdateCart` (qty=0) |
| `EventPurchase`      | `Checkout`           |
| `EventCancelOrder`   | `CancelOrder`        |
| `EventRefundReq`     | `CreateRefund`       |

---
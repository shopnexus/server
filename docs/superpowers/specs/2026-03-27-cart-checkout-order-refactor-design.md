# Cart, Checkout & Order Refactor Design

## Summary

Refactor the order module to decouple checkout from order creation. Cart stays flat (current `cart_item`). Checkout creates pending `order.item` records with reserved inventory but no order or payment. Sellers confirm and group items into orders with transport. Buyers pay confirmed orders with exact totals. Shipment is replaced by a generic transport system supporting physical shipping, digital delivery, and more.

## Key Decisions

- Cart stays flat (`cart_item`: account_id, sku_id, quantity) — unchanged
- `order.item` exists before `order` — created at checkout, grouped into orders at seller confirmation
- Payment happens after seller confirmation — buyer sees exact total (product + transport) before paying
- Inventory reserved at checkout (prevents overselling; TODO: auto-expire via Restate durable workflow)
- `order.shipment` replaced by generic `order.transport` with JSONB `data` for provider-specific metadata
- Naming: `buyer` / `seller` (not customer/vendor)
- Seller can only group items with same buyer AND same address
- Cross-payment combining: not applicable (payment is per-order now)
- Buy Now kept: bypasses cart, directly creates pending items
- Manual cancel only for now (TODO: auto-expire unconfirmed items via Restate)

## Schema Changes

### New Enums

```sql
CREATE TYPE "order"."item_status" AS ENUM ('Pending', 'Confirmed', 'Canceled');
```

Rename existing enum: `order.shipment_status` → `order.transport_status` (values unchanged: Pending, LabelCreated, InTransit, OutForDelivery, Delivered, Failed, Cancelled).

### New Table: `order.transport` (replaces `order.shipment`)

```sql
CREATE TABLE "order"."transport" (
    "id"           UUID NOT NULL DEFAULT gen_random_uuid(),
    "option"       TEXT NOT NULL,
    "status"       "order"."transport_status" NOT NULL DEFAULT 'Pending',
    "cost"         BIGINT NOT NULL DEFAULT 0,
    "data"         JSONB NOT NULL DEFAULT '{}',
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "transport_pkey" PRIMARY KEY ("id")
);
```

Provider-specific fields (tracking_code, label_url, from_address, to_address, weight, dimensions, ETA) move into `data` JSONB.

### Modified: `order.item`

Added fields:

| Field | Type | Description |
|-------|------|-------------|
| `account_id` | UUID NOT NULL | Buyer who requested the item |
| `seller_id` | UUID NOT NULL | Seller who owns the product (derived from SKU→SPU at checkout) |
| `address` | TEXT NOT NULL | Delivery address per item |
| `status` | item_status DEFAULT 'Pending' | Pending → Confirmed → Canceled |
| `paid_amount` | BIGINT NOT NULL DEFAULT 0 | Actual amount (TODO: after promotion) |
| `date_created` | TIMESTAMPTZ(3) DEFAULT CURRENT_TIMESTAMP | When checkout was requested |
| `date_updated` | TIMESTAMPTZ(3) DEFAULT CURRENT_TIMESTAMP | Updated on status transitions |

Existing fields kept as-is: `id`, `sku_id`, `sku_name`, `quantity`, `unit_price`, `note`, `serial_ids`.

Modified fields:

| Field | Change |
|-------|--------|
| `order_id` | Now NULLABLE (null = pending confirmation). FK to `order.order` kept — NULLs are allowed by FK semantics. |

Removed constraints:

- Drop `UNIQUE(order_id, sku_id)`

New indexes:

```sql
CREATE INDEX item_pending_seller_idx ON "order"."item" ("seller_id", "status") WHERE "order_id" IS NULL;
CREATE INDEX item_pending_account_idx ON "order"."item" ("account_id", "status") WHERE "order_id" IS NULL;
```

### Modified: `order.order`

Renamed fields:

| Old | New |
|-----|-----|
| `customer_id` | `buyer_id` |
| `vendor_id` | `seller_id` |
| `shipment_id` | `transport_id` (FK → `order.transport`) |
| `ship_cost` | `transport_cost` |

Modified fields:

| Field | Change |
|-------|--------|
| `payment_id` | Now NULLABLE (null until buyer pays after confirmation) |

Removed fields:

| Field | Reason |
|-------|--------|
| `ship_discount` | No transport discount |

Kept fields: `id`, `confirmed_by_id`, `status`, `address`, `product_cost`, `product_discount`, `transport_cost`, `total`, `note`, `data`, `date_created`.

Cost formula: `total = product_cost - product_discount + transport_cost`

### Modified: `order.payment`

No changes. Payment stays as-is. Created when buyer pays a confirmed order.

### Unchanged: `order.cart_item`

Stays flat: `account_id`, `sku_id`, `quantity`.

### Dropped: `order.shipment`

Replaced by `order.transport`.

## Flows

### Flow 1: Checkout (Purchase Request)

**Input**: account, items: [{sku_id, quantity, address, note}], buy_now flag

1. Fetch SKU details from catalog (price, name, can_combine, SPU's account_id as seller_id)
2. Reserve inventory via inventory module (assigns serial IDs)
3. Create `order.item` for each SKU:
   - `account_id` = buyer
   - `seller_id` = from SPU
   - `address` = per item
   - `sku_id`, `sku_name`, `quantity`, `unit_price`, `paid_amount`
   - `serial_ids` from inventory reservation
   - `status` = Pending, `order_id` = NULL
4. Remove items from cart (skip if Buy Now)
5. Return list of created items

No payment, no order, no transport created at this stage.

### Flow 2: Seller Incoming View

**Query**: `order.item WHERE seller_id = ? AND status = 'Pending'`

**Response grouped by**: buyer (`account_id`) → address → SPU (derived from SKU) → individual items

Grouping by address ensures the seller can see which items are combinable (same buyer + same address). Each item shows: buyer info, address, SKU name/attributes, quantity, unit_price.

### Flow 3: Seller Confirmation (Create Order)

**Input**: seller_id, item_ids (selected pending items), transport_option

1. Validate all items have same `account_id` (buyer) and same `address`
2. Validate all items have `status = 'Pending'` and `seller_id` matches
3. Create `order.transport`:
   - Provider calculates cost from item metadata
   - `option` = selected transport provider
   - `cost` = calculated cost
   - `data` = provider-specific metadata
4. Create `order.order`:
   - `buyer_id` = from items
   - `seller_id` = confirming seller
   - `address` = from items (all match)
   - `transport_id` = new transport
   - `product_cost` = sum of items' `unit_price * quantity`
   - `product_discount` = `product_cost - sum(paid_amount)` (difference reflects promotions)
   - `transport_cost` = transport.cost
   - `total` = product_cost - product_discount + transport_cost
   - `payment_id` = NULL (not paid yet)
   - `status` = Pending
   - `confirmed_by_id` = seller
   - `note` = seller-provided (optional, set at confirmation time)
   - `data` = defaults to `'{}'` (reserved for future use)
5. Update selected items: `order_id` = new order, `status` = Confirmed
6. Notify buyer: "Order confirmed, total = X"

### Flow 4: Buyer Payment

**Input**: account, order_ids (confirmed orders to pay), payment_option

1. Validate all orders belong to buyer, status = Pending, payment_id = NULL
2. Create `payment`:
   - `account_id` = buyer
   - `amount` = sum of selected orders' totals
   - `option` = buyer's choice
3. Call payment provider (VNPay → redirect URL, COD → auto-success)
4. Set `order.payment_id` on all selected orders
5. On payment success: payment status → Success

One payment can cover multiple orders (buyer pays N confirmed orders at once).

### Flow 5: Cancellation

| Who | When | Action |
|-----|------|--------|
| Buyer | Item is Pending (before confirmation) | Cancel item, release inventory |
| Buyer | Order confirmed but not paid | Cancel order, release inventory, items → Canceled |
| Buyer | Order paid but not fulfilled | Cancel order, release inventory, initiate refund |
| Seller | Item is Pending | Reject item, release inventory, notify buyer |

TODO: Auto-expire unconfirmed items via Restate durable workflow (future).

## API Endpoints

All under `/api/v1/order`:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| **Cart** | | | |
| GET | `/cart` | Yes | Get cart items |
| POST | `/cart` | Yes | Update cart item (add/update/remove) |
| DELETE | `/cart` | Yes | Clear cart |
| **Checkout** | | | |
| POST | `/checkout` | Yes | Request checkout → create pending items |
| **Pending Items (buyer)** | | | |
| GET | `/checkout/items` | Yes | List my pending/confirmed items |
| DELETE | `/checkout/items/:id` | Yes | Cancel a pending item |
| **Incoming Items (seller)** | | | |
| GET | `/incoming` | Yes | List pending items for seller |
| POST | `/incoming/confirm` | Yes | Confirm & group items into order |
| POST | `/incoming/reject` | Yes | Reject pending items |
| **Orders** | | | |
| GET | `/order` | Yes | List my orders (as buyer) |
| GET | `/order/:id` | Yes | Get order detail |
| GET | `/order/seller` | Yes | List orders as seller |
| **Payment** | | | |
| POST | `/order/pay` | Yes | Pay confirmed order(s) |
| GET | `/order/ipn` | No | Payment provider callback (IPN) |
| **Refund** | | | |
| GET | `/refund` | Yes | List refunds |
| POST | `/refund` | Yes | Create refund |
| PATCH | `/refund` | Yes | Update refund |
| DELETE | `/refund` | Yes | Cancel refund |
| POST | `/refund/confirm` | Yes | Seller confirms refund |

## Transport Provider Interface

```go
type Client interface {
    Config() sharedmodel.OptionConfig
    Quote(ctx context.Context, params QuoteParams) (QuoteResult, error)
    Create(ctx context.Context, params CreateParams) (Transport, error)
    Track(ctx context.Context, id string) (TrackResult, error)
    Cancel(ctx context.Context, id string) error
}

type QuoteParams struct {
    Items       []ItemMetadata
    FromAddress string // seller's address
    ToAddress   string // buyer's address
}

// ItemMetadata contains provider-relevant info about an item.
// Physical shipping providers use weight/dimensions.
// Digital providers use file size/type.
type ItemMetadata struct {
    SkuID          uuid.UUID
    Quantity       int64
    PackageDetails json.RawMessage // from catalog SKU
}

type CreateParams struct {
    Items       []ItemMetadata
    FromAddress string // seller's address
    ToAddress   string // buyer's address (from order.item)
    Option      string // transport option ID
}

type QuoteResult struct {
    Cost int64
    Data json.RawMessage // provider-specific (ETA, dimensions, etc.)
}

type Transport struct {
    ID     uuid.UUID
    Option string
    Cost   int64
    Data   json.RawMessage
}
```

Existing GHTK shipping variants become transport provider implementations. New providers (cloud transfer, digital download) implement the same interface. Provider-specific metadata lives in `data` JSONB.

## What Gets Removed

- `order.shipment` table → replaced by `order.transport`
- `ship_cost`, `ship_discount` on order → `transport_cost` (no discount)
- Old checkout flow (atomically creating payment + order + shipment)
- Shipment provider setup → transport provider setup

## What Stays Unchanged

- `order.cart_item` (flat cart)
- `order.refund` (FK `shipment_id` renamed to `transport_id`, references `order.transport`)
- `order.refund_dispute`
- Payment provider interface (VNPay, COD)
- Inventory reserve logic (note: `ReleaseInventory` must be added to `InventoryBiz` — currently only `ReserveInventory` exists)
- Buy Now (bypasses cart, creates pending items directly)

## Method Renames

| Old                | New                                                              |
|--------------------|------------------------------------------------------------------|
| `ListVendorOrder`  | `ListSellerOrders`                                               |
| `ConfirmOrder`     | `ConfirmItems` (seller confirms pending items, creates order)    |
| `Checkout`         | `Checkout` (now creates pending items, not orders)               |
| `QuoteOrder`       | removed (transport cost determined at confirmation, not checkout) |

## Notes

- **Migration**: This is a breaking schema change. Existing orders and items need data backfill (populate `account_id`, `seller_id`, `address`, `status=Confirmed` on existing items; rename columns on orders). Write a single migration with backfill.
- **Idempotency**: Checkout should use Restate invocation IDs to prevent duplicate it em creation on double-submit.
- **Partial confirmation**: Unconfirmed items stay Pending indefinitely until manually canceled or auto-expired (future TODO).
- **Seller notifications**: Fire-and-forget notification to seller when new pending items arrive (via `restate.ServiceSend`).
- **IPN endpoint**: `GET /order/ipn` — VNPay sends payment callback data via query params. No auth required.

## TODOs (Future)

- Auto-expire unconfirmed items via Restate durable workflow
- Promotion system: apply to `paid_amount`, discount the largest-priced item
- `ReleaseInventory` method in inventory module (for cancel/expire)
- Seller notification on new pending items

# Order Module

Manages the full order lifecycle: cart, checkout, seller confirmation, payment, cancellation, and refunds.

**Struct:** `OrderHandler` | **Interface:** `OrderBiz` | **Restate service:** `Order`

## Key Concepts

- **No customer/vendor distinction** -- any account can buy and sell. Orders track `buyer_id` and `seller_id` per transaction.
- **Checkout creates pending items**, not orders. Inventory is reserved and cart items are removed.
- **Sellers create orders** by confirming incoming pending items via `ConfirmItems`, which creates a transport and groups items into an order.
- **Payment is separate** -- `payment_id` on orders is nullable until the buyer calls `PayOrders`.
- **Pluggable providers** -- payment and transport providers are registered at startup in `map[string]Client` maps, selected by option string.

## Order Flow

```
Cart -> Checkout (pending items) -> Seller confirms (creates order + transport)
     -> Buyer pays (PayOrders) -> Delivery -> (optional) Refund
```

1. **Checkout**: reserves inventory, removes from cart, creates pending `order.item` records (no order yet)
2. **Pending items**: buyer can list and cancel pending items (releases inventory)
3. **Incoming items**: seller sees pending items, selects items, picks transport option, creates order via `ConfirmItems`
4. **Payment**: buyer pays confirmed orders via `PayOrders` (creates payment, calls provider)
5. **Cancel**: buyer can cancel unpaid orders (releases inventory)
6. **Refund**: buyer requests refund on paid orders (PickUp/DropOff methods)

## Tables

`order.cart_item`, `order.item`, `order.order`, `order.payment`, `order.transport`, `order.refund`, `order.refund_dispute`

## Providers

**Payment:** VNPay (QR/Bank/ATM), COD (`system-cod`)

**Transport:** GHTK (Express/Standard/Economy) -- mock implementation with cost based on weight and service tier

## API Endpoints

### Cart

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| GET | `/api/v1/order/cart` | GetCart | Yes | List cart items for authenticated user |
| POST | `/api/v1/order/cart` | UpdateCart | Yes | Add/update/remove cart item (quantity or delta) |
| DELETE | `/api/v1/order/cart` | ClearCart | Yes | Remove all cart items |

### Checkout & Pending Items

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| POST | `/api/v1/order/checkout` | Checkout | Yes | Checkout items, reserve inventory, create pending items |
| GET | `/api/v1/order/checkout/items` | ListPendingItems | Yes | List buyer's pending items |
| DELETE | `/api/v1/order/checkout/items/:id` | CancelPendingItem | Yes | Cancel a pending item (releases inventory) |

### Incoming Items (Seller)

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| GET | `/api/v1/order/incoming` | ListIncomingItems | Yes | List pending items for seller's products |
| POST | `/api/v1/order/incoming/confirm` | ConfirmItems | Yes | Confirm items, create transport + order |
| POST | `/api/v1/order/incoming/reject` | RejectItems | Yes | Reject pending items (releases inventory) |

### Orders & Payment

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| GET | `/api/v1/order` | ListOrders | Yes | List buyer's orders with pagination |
| GET | `/api/v1/order/seller` | ListSellerOrders | Yes | List seller's orders with status filters |
| GET | `/api/v1/order/:id` | GetOrder | Yes | Get order by ID |
| POST | `/api/v1/order/pay` | PayOrders | Yes | Pay for confirmed orders |
| GET | `/api/v1/order/ipn` | VnpayVerifyIPN | No | VNPay IPN callback |

### Refunds

| Method | Path | Handler | Auth | Description |
|--------|------|---------|------|-------------|
| GET | `/api/v1/order/refund` | ListRefunds | Yes | List refund requests |
| POST | `/api/v1/order/refund` | CreateRefund | Yes | Create refund request (PickUp/DropOff) |
| PATCH | `/api/v1/order/refund` | UpdateRefund | Yes | Update pending refund |
| DELETE | `/api/v1/order/refund` | CancelRefund | Yes | Cancel pending refund |
| POST | `/api/v1/order/refund/confirm` | ConfirmRefund | Yes | Seller confirms refund |

## Cross-Module Dependencies

| Module | Usage |
|--------|-------|
| `account` | Authenticated identity, seller default contacts |
| `catalog` | SPU/SKU lookup, pricing, package details |
| `inventory` | Reserve/release inventory during checkout |
| `promotion` | Price calculation with promotion codes |
| `common` | Resource management (refund images) |

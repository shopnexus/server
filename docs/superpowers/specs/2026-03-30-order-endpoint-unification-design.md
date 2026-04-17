# Order Endpoint Unification Design

## Goal

Restructure order HTTP endpoints into a clear `buyer/seller` hierarchy and rename biz methods to match. Add combined "overview" responses in the frontend so each role sees pending items and confirmed orders on one page.

## Current State

The order module has 4 list endpoints with inconsistent naming:

| Endpoint | Purpose |
|---|---|
| `GET /order/checkout/items` | Buyer's pending items |
| `GET /order` | Buyer's confirmed orders |
| `GET /order/incoming` | Seller's incoming items |
| `GET /order/seller` | Seller's confirmed orders |

Action endpoints are similarly scattered (`POST /order/checkout`, `POST /order/incoming/confirm`, etc.).

## New Endpoint Structure

All order endpoints reorganized under `/api/v1/order/{role}/...`:

### Cart (buyer-only, no role prefix)

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/order/cart` | Get cart |
| `POST` | `/order/cart` | Add/update cart item |
| `DELETE` | `/order/cart` | Clear cart |

### Buyer - Pending Items

| Method | Endpoint | Biz Method | Description |
|---|---|---|---|
| `POST` | `/order/buyer/checkout` | `BuyerCheckout` | Create pending items from cart/buy-now |
| `GET` | `/order/buyer/pending` | `ListBuyerPending` | List pending items |
| `DELETE` | `/order/buyer/pending/:id` | `CancelBuyerPending` | Cancel a pending item |

### Buyer - Confirmed Orders

| Method | Endpoint | Biz Method | Description |
|---|---|---|---|
| `GET` | `/order/buyer/confirmed` | `ListBuyerConfirmed` | List confirmed orders |
| `GET` | `/order/buyer/confirmed/:id` | `GetBuyerOrder` | Get single order detail |
| `DELETE` | `/order/buyer/confirmed/:id` | `CancelBuyerOrder` | Cancel order (if still pending) |
| `POST` | `/order/buyer/pay` | `PayBuyerOrders` | Pay for orders |

### Buyer - Refunds

| Method | Endpoint | Biz Method | Description |
|---|---|---|---|
| `GET` | `/order/buyer/refund` | `ListBuyerRefunds` | List my refunds |
| `POST` | `/order/buyer/refund` | `CreateBuyerRefund` | Create refund request |
| `PATCH` | `/order/buyer/refund` | `UpdateBuyerRefund` | Update refund |
| `DELETE` | `/order/buyer/refund` | `CancelBuyerRefund` | Cancel refund |

### Seller - Pending Items

| Method | Endpoint | Biz Method | Description |
|---|---|---|---|
| `GET` | `/order/seller/pending` | `ListSellerPending` | List incoming items |
| `POST` | `/order/seller/pending/confirm` | `ConfirmSellerPending` | Confirm items, create order |
| `POST` | `/order/seller/pending/reject` | `RejectSellerPending` | Reject items |

### Seller - Confirmed Orders

| Method | Endpoint | Biz Method | Description |
|---|---|---|---|
| `GET` | `/order/seller/confirmed` | `ListSellerConfirmed` | List seller orders |
| `GET` | `/order/seller/confirmed/:id` | `GetSellerOrder` | Get single order detail |

### Seller - Refunds

| Method | Endpoint | Biz Method | Description |
|---|---|---|---|
| `POST` | `/order/seller/refund/confirm` | `ConfirmSellerRefund` | Confirm refund |

### Unchanged (internal/cross-module)

These methods are not exposed via the buyer/seller endpoints and keep their current names:

- `ConfirmPayment` — payment webhook callback
- `HasPurchasedProduct`, `ListReviewableOrders`, `ValidateOrderForReview` — review helpers
- `GetSellerOrderStats`, `GetSellerOrderTimeSeries`, `GetSellerPendingActions`, `GetSellerTopProducts` — dashboard analytics

## Biz Method Rename Map

### Buyer

| Old | New |
|---|---|
| `Checkout` | `BuyerCheckout` |
| `ListPendingItems` | `ListBuyerPending` |
| `CancelPendingItem` | `CancelBuyerPending` |
| `ListOrders` | `ListBuyerConfirmed` |
| `GetOrder` | `GetBuyerOrder` |
| `CancelOrder` | `CancelBuyerOrder` |
| `PayOrders` | `PayBuyerOrders` |
| `ListRefunds` | `ListBuyerRefunds` |
| `CreateRefund` | `CreateBuyerRefund` |
| `UpdateRefund` | `UpdateBuyerRefund` |
| `CancelRefund` | `CancelBuyerRefund` |

### Seller

| Old | New |
|---|---|
| `ListIncomingItems` | `ListSellerPending` |
| `ConfirmItems` | `ConfirmSellerPending` |
| `RejectItems` | `RejectSellerPending` |
| `ListSellerOrders` | `ListSellerConfirmed` |
| *(new — shared logic with GetBuyerOrder)* | `GetSellerOrder` |
| `ConfirmRefund` | `ConfirmSellerRefund` |

### Implementation Note

`GetBuyerOrder` and `GetSellerOrder` share the same underlying logic (fetch order by ID with items, payment, transport). The transport layer performs different auth checks: buyer must be `order.buyer_id`, seller must be `order.seller_id`. Both biz methods can delegate to a shared private `getOrder` helper.

## Frontend Changes

### New Hooks

In `core/order/order.buyer.ts`:
- `useGetBuyerOverview()` — calls both `GET /order/buyer/pending` and `GET /order/buyer/confirmed` in parallel, returns `{ pendingItems, orders }`

In `core/order/order.seller.ts`:
- `useGetSellerOverview(params?)` — calls both `GET /order/seller/pending` and `GET /order/seller/confirmed` in parallel, returns `{ incomingItems, orders }`

All existing hooks updated to use new endpoint URLs.

### Page Changes

**Buyer:** Update `/account/orders/` page to show both sections using the overview hook — "Pending Items" section at top, "Orders" section below. Each has a "View all" link to the existing granular pages for full pagination/filtering.

**Seller:** Update `/seller/orders/` (or seller landing) to show both "Incoming Items" and "Orders" sections from the overview hook. Each section shows latest entries with "View all" link.

Existing granular pages (`/account/pending-items/`, `/seller/incoming/`, `/seller/orders/`) remain for full pagination, filtering, and bulk actions.

## Error Handling

Follows existing project patterns:
- Auth failures: 401
- Validation errors: `sharedmodel.NewError(400, msg).Terminal()`
- Not found: `sharedmodel.NewError(404, msg).Terminal()`
- Internal errors: `sharedmodel.WrapErr(msg, err)` to preserve terminal flag
- No partial responses on overview — if either inner call fails, the frontend query fails

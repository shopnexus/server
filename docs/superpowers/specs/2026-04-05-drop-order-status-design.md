# Drop `order.status` Column

**Date:** 2026-04-05
**Status:** Approved

## Problem

The `order.order.status` column is redundant. Orders are created as `Pending` and only ever transition to `Cancelled`. The real state lives in child entities: `payment.status`, `transport.status`, and `item.status`. The column adds confusion and no value.

## Decision

Drop `order.status` entirely. The frontend infers display state from the child entity statuses already present in the API response. No backend status computation.

## Changes

### Database

- **Migration**: `ALTER TABLE "order"."order" DROP COLUMN "status";`
- **Keep** `order.status` enum — still used by `payment`, `refund`, `refund_dispute` tables.
- **Keep** `order.item_status` enum — still used by `order.item`.

### Backend — Deletions

| File | What to remove |
|------|---------------|
| `biz/order.go` | Delete `CancelBuyerOrder()` entirely. Remove `Status` field from `ListBuyerConfirmedParams` and its usage in the query call. |
| `biz/incoming.go` | Remove `Status: OrderStatusPending` from the `CreateDefaultOrder` call. |
| `biz/pay.go` | Remove `order.status == Pending` check in `PayBuyerOrders`. Replace with checking `payment_id IS NULL`. |
| `transport/echo/order.go` | Remove `Status` from `ListBuyerConfirmedRequest`. Remove `CancelBuyerOrder` handler and route. Remove `order_status` filter from `ListSellerConfirmedRequest`. |
| `db/queries/` | Remove `status` from order INSERT, UPDATE, SELECT queries. Remove status-based WHERE clauses on the order table. |
| `model/` | Remove `Status` field from order response model (if present). |

### Backend — Keep unchanged

- `item.status` lifecycle: Pending -> Confirmed / Cancelled
- `payment.status` lifecycle: Pending -> Success / Failed / Cancelled
- `transport.status` lifecycle: Pending -> LabelCreated -> InTransit -> OutForDelivery -> Delivered / Failed / Cancelled
- Refund flow — untouched
- `CancelBuyerPending()` — still works (cancels individual items before seller confirms)

### Frontend

| File | What to change |
|------|---------------|
| `core/order/order.buyer.ts` | Remove `status` from `TOrder` type. Remove `status` param from `useListBuyerConfirmed`. Remove `useCancelBuyerOrder` if it exists. |
| `core/order/order.seller.ts` | Remove `order_status` param from `useListSellerConfirmed`. |
| `account/orders/page.tsx` | Buyer order tabs currently filter by `status: ["Pending", "Processing"]` etc. Refactor to filter by `payment_status` or remove server-side filtering and filter client-side from `order.payment.status` + `order.transport?.status`. |
| `seller/orders/page.tsx` | Remove order_status filter from confirmed tab. |
| `_components/order-list.tsx` | Derive display badge from `order.payment?.status` and transport status instead of `order.status`. |

### Frontend status inference (in components, not a shared function)

| payment.status | transport.status | Display |
|---------------|-----------------|---------|
| NULL (no payment) | * | Unpaid |
| Pending | * | Awaiting Payment |
| Failed | * | Payment Failed |
| Cancelled | * | Cancelled |
| Success | Pending / LabelCreated | Processing |
| Success | InTransit / OutForDelivery | Shipping |
| Success | Delivered | Completed |
| Success | Failed / Cancelled | Delivery Failed |

## What this does NOT change

- No new enums or status types on the backend
- No status computation on the backend
- Refund, refund_dispute, payment, transport, and item tables are untouched
- Checkout and pending item flows are untouched

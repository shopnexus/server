# Order Tab Endpoints Refactor

**Date:** 2026-04-30
**Owner:** Khoa Bean
**Status:** Draft (awaiting user review)

## Problem

The buyer's order page has three tabs (Pending / Completed / Cancelled) but only two BE endpoints feed them. The Cancelled tab filters client-side from the same `ListBuyerConfirmed` stream that feeds Pending and Completed, and the pre-confirm "items without an order row yet" never reach the Cancelled tab at all — failed-checkout items remain stuck under Pending forever because `ListBuyerPendingItems` does not check `payment_session.status`. The Completed tab uses `transport.status = 'Delivered'`, which does not match the domain truth: an order is only "done" when the seller payout is released, not when the parcel is delivered.

The seller side has only two tabs and is structurally fine, but the underlying handler and SQL are tangled in `order.go` / `pending_buyer.go` / `order.sql` together with buyer code, which makes the buyer refactor harder than it needs to be.

## Goals

- Replace ad-hoc client-side filtering with explicit per-tab BE endpoints.
- Fix tab semantics: failed checkouts land under Cancelled; "Completed" means escrow released.
- Split buyer and seller handlers + SQL into domain-named files.
- Keep each endpoint's response a native shape (`OrderItem` or `Order`), not a polymorphic union.

## Non-goals

- `CancelBuyerPending` workflow logic. Its terminal-error guard for already-Failed sessions stays as a safety net.
- Saga compensator behavior. Current behavior — flip session/tx status to Failed, leave item rows untouched — is correct and remains.
- Post-delivery dispute / refund affecting order status. Will need a separate spec.

## Endpoint inventory

### Buyer — 5 endpoints

| Method | Returns | Filter |
|---|---|---|
| `ListBuyerPendingItems` | `[]OrderItem` | `account_id = @buyer AND order_id IS NULL AND date_cancelled IS NULL AND ps.status IN ('Pending', 'Success')` |
| `ListBuyerPendingOrders` | `[]Order` | `buyer_id = @buyer AND NOT order.is_cancelled(...) AND payout_session.status IS DISTINCT FROM 'Success'` |
| `ListBuyerCompletedOrders` | `[]Order` | `buyer_id = @buyer AND payout_session.status = 'Success' AND NOT order.is_cancelled(...)` |
| `ListBuyerCancelledItems` | `[]OrderItem` | `account_id = @buyer AND order_id IS NULL AND (ps.status IN ('Failed', 'Cancelled') OR date_cancelled IS NOT NULL)` |
| `ListBuyerCancelledOrders` | `[]Order` | `buyer_id = @buyer AND order.is_cancelled(...)` |

`order.is_cancelled(confirm_status, transport_status, payout_status)` returns true if any of the three is in `('Failed', 'Cancelled')`. Mutual exclusion priority: **Cancelled > Completed > Pending**.

### Seller — 2 endpoints

| Method | Returns | Filter |
|---|---|---|
| `ListSellerIncomingItems` | `[]OrderItem` | `seller_id = @seller AND order_id IS NULL AND date_cancelled IS NULL AND ps.status = 'Success'` (rename of current `ListSellerPendingItems`) |
| `ListSellerConfirmedOrders` | `[]Order` | `seller_id = @seller` (rename of current `ListSellerConfirmed` — no semantic change) |

Seller side does NOT split by completed/cancelled. The `Confirmed` tab keeps showing all post-confirm orders with a status badge per row.

## Data model

The Cancelled predicate becomes a SQL function appended to `0001_init.up.sql`. All three columns share the single `order.status` enum already defined at line 16. Function returns NOT NULL via `COALESCE` — both `transport.status` and the joined `payout_session.status` can be NULL (transport row exists with NULL status pre-shipment; payout row may not exist at all):

```sql
CREATE FUNCTION "order".is_cancelled(
    confirm_status "order"."status",
    transport_status "order"."status",
    payout_status   "order"."status"
) RETURNS BOOLEAN
LANGUAGE SQL IMMUTABLE
AS $$
    SELECT COALESCE(confirm_status   IN ('Failed', 'Cancelled'), FALSE)
        OR COALESCE(transport_status IN ('Failed', 'Cancelled'), FALSE)
        OR COALESCE(payout_status    IN ('Failed', 'Cancelled'), FALSE);
$$;
```

Order queries use these joins (no FK on payment_session, but `PayoutWorkflow` sets `payment_session.id = order.id` for the payout — see `workflow_payout.go:51`):

```sql
FROM "order"."order" o
LEFT JOIN "order"."payment_session" ps_confirm
       ON ps_confirm."id" = o."confirm_session_id"
LEFT JOIN "order"."payment_session" ps_payout
       ON ps_payout."id" = o."id" AND ps_payout."kind" = 'seller-payout'
LEFT JOIN "order"."transport" t ON t."id" = o."transport_id"
```

`GetPendingPayoutSessionForOrder` (in `payment_session.sql`) currently matches on `to_id = seller_id` rather than `id = order_id`, which returns ANY pending payout for the seller — not specifically this order's. As part of this refactor, **`GetPendingPayoutSessionForOrder` is rewritten to `GetPayoutSessionForOrder` matching on `id = @order_id AND kind = 'seller-payout'`**, returning the row regardless of status. Hydrate code (`order_hydrate.go`) updates accordingly.

`hydrateOrders` in `order_hydrate.go` switches from `GetPendingPayoutSessionForOrder` (only loads pending payouts) to a new `GetPayoutSessionForOrder` that loads the latest payout regardless of status. This lets the FE render a "Funds released" badge for Completed orders.

## File structure

### Backend

```
internal/module/order/
├── biz/
│   ├── order_buyer.go         # NEW — 5 buyer List* methods + GetBuyerOrder
│   ├── order_seller.go        # NEW — 2 seller List* methods + GetSellerOrder
│   ├── order_hydrate.go       # NEW — hydrateOrders, enrichItems shared
│   ├── pending_buyer.go       # KEEP — only CancelBuyerPending + RefundPendingItem
│   ├── pending_seller.go      # KEEP
│   ├── order.go               # DELETE — content moves to order_buyer/seller/hydrate
│   └── ... (other files unchanged)
├── db/queries/
│   ├── order_buyer.sql        # NEW — 3 buyer order queries
│   ├── order_seller.sql       # NEW — content moved from order.sql
│   ├── item_custom.sql        # UPDATE — ListBuyerPendingItems filter; new ListBuyerCancelledItems; rename ListSellerPendingItems
│   ├── payment_session.sql    # UPDATE — add GetPayoutSessionForOrder
│   └── order.sql              # DELETE
└── db/migrations/
    └── 0001_init.up.sql       # APPEND — order.is_cancelled function
```

### Frontend

```
shopnexus-website-v3/
├── core/order/
│   ├── order.buyer.ts         # 5 hooks: useListBuyerPendingItems / PendingOrders / CompletedOrders / CancelledItems / CancelledOrders
│   └── order.seller.ts        # 2 hooks: useListSellerIncomingItems / ConfirmedOrders
├── app/(marketplace)/account/orders/
│   ├── page.tsx               # 3 tabs, mixed tabs render two sections
│   └── _components/
│       ├── pending-item-card.tsx   # NEW — extract from page.tsx, fix cancel-button gating
│       ├── order-list.tsx          # KEEP
│       └── item-list.tsx           # NEW — render TOrderItem[] with cancellable / readOnly variants
├── app/(marketplace)/seller/orders/
│   └── page.tsx               # rename hook, no logic change
└── lib/
    └── order-status.ts        # SHRINK — drop isCancelledOrder/isCompletedOrder/isActiveOrder; keep only badge label helpers if needed
```

## Tab structure

```tsx
// Buyer
<TabsContent value="pending">
  <Section title="Awaiting Payment / Seller Confirm">
    <ItemList hook={useListBuyerPendingItems} cancellable />
  </Section>
  <Section title="Awaiting Delivery & Payout">
    <OrderList hook={useListBuyerPendingOrders} />
  </Section>
</TabsContent>

<TabsContent value="completed">
  <OrderList hook={useListBuyerCompletedOrders} />
</TabsContent>

<TabsContent value="cancelled">
  <Section title="Cancelled Items (pre-confirm)">
    <ItemList hook={useListBuyerCancelledItems} readOnly />
  </Section>
  <Section title="Cancelled Orders">
    <OrderList hook={useListBuyerCancelledOrders} />
  </Section>
</TabsContent>
```

Each section paginates independently (own Load More button), matching the existing Pending tab pattern. No client-side merge sort.

`PendingItemCard` cancel button gating, with defense-in-depth even though the BE filter now hides Failed/Cancelled sessions from the Pending feed:

```tsx
const isTerminal = txStatus === "Failed" || txStatus === "Cancelled"
{!item.order_id && !item.date_cancelled && !isTerminal && (
  <Button>Cancel</Button>
)}
```

## SQL strategy

Each query uses the established `sqlc.embed(...) + COUNT(*) OVER() AS total_count` pattern so the FE paginator (`useInfiniteQueryPagination`) needs no refactor. Order queries `LEFT JOIN` payment_session twice (confirm + payout) and pass all three statuses to `order.is_cancelled(...)`. Item queries `JOIN payment_session` on `payment_session_id`.

Count-only queries (`CountBuyerPendingItems`, `ListCountBuyerOrder`, `ListCountSellerOrder`) are removed — every list query embeds total via window function.

## Restate

After updating `interface.go` with the 7 new methods (5 buyer + 2 seller), run `go generate ./internal/module/order/biz/` to regenerate `restate_gen.go`. Each method takes a single `<Method>Params` struct as required by `genrestate`.

## Implementation order

1. **Migration append** — add `order.is_cancelled` to `0001_init.up.sql`, run `make migrate`.
2. **SQL queries** — write 5 buyer queries + `GetPayoutSessionForOrder`, run `make pgtempl` then `sqlc generate`. Old queries stay temporarily.
3. **BE handlers** — write `order_buyer.go`, `order_seller.go`, `order_hydrate.go`. Update `interface.go` + `restate_gen.go`. Old handlers stay temporarily.
4. **FE adopt** — write 5 buyer hooks, 2 seller hooks. Refactor `page.tsx` (buyer + seller). E2E test on dev.
5. **BE remove old** — delete `ListBuyerConfirmed`, `ListSellerConfirmed`, `ListCountBuyerOrder`, `ListCountSellerOrder`, `CountBuyerPendingItems`, old `ListBuyerPendingItems` filter. Re-run restate gen.
6. **Seller rename** — Khoa handles the `Pending → Incoming` rename across BE + FE + sqlc + restate proxy.

On dev DB, steps 1–6 can land in one go. For staged rollout, ship 1–4 together, monitor, then 5–6.

## Risks

- **Pagination across two sections in one tab**: each section caches independently; a cancellation moving an item from Pending → Cancelled requires invalidating two query keys. The existing `useInfiniteQueryPagination` + tag-based invalidation in `customFetchStandard` handles this — the cancel mutation needs to invalidate the new keys (`buyer/pending/items`, `buyer/cancelled/items`).
- **Payout session loaded for every order** in `hydrateOrders` adds one query per order. Acceptable at current scale; can batch later if needed.
- **Seller rename** is wide and mechanical. Khoa drives it to avoid agent-induced search-and-replace mistakes.

## Open questions resolved during brainstorm

- **Q1 — Response shape:** Native item / order types per endpoint. No polymorphic union. (Selected over tagged union, two-arrays envelope, normalized Purchase shape, item-centric.)
- **Q2 — Cancelled bucket split:** Symmetric — `CancelledItems` + `CancelledOrders` separate endpoints, mirroring Pending split.
- **Q3 — Order priority:** Cancelled > Completed > Pending. Cancelled query checked first; other queries exclude the cancelled set.
- **Q3 follow-up — "Completed" semantics:** Payout released (`payout_session.status = 'Success'`), not delivery. Delivered-but-not-yet-paid-out stays Pending.
- **Q4 — Pagination per tab:** Two independent paginators per mixed tab, each with its own Load More.
- **Migration placement:** Append to `0001_init.up.sql`, no new migration file (dev DB, additive).
- **Cancelled predicate location:** SQL function `order.is_cancelled(...)` — DRY, single point of change for future rule additions.
- **Seller rename:** `ListSellerPendingItems → ListSellerIncomingItems`, `ListSellerConfirmed → ListSellerConfirmedOrders`. Khoa handles mechanically.

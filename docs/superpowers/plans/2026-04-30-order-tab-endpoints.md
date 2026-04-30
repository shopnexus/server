# Order Tab Endpoints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the buyer's tangled three-tab order page with five role-specific BE endpoints (pending-items, pending-orders, completed-orders, cancelled-items, cancelled-orders); rename and tidy the seller's two endpoints; rewire the FE to consume them with two-section mixed tabs.

**Architecture:** Buyer's pre-confirm purchases live in `order.item` with `order_id IS NULL`; post-confirm purchases live in `order.order`. Each tab consumes one or two of these streams via dedicated paginated endpoints. A new `order.is_cancelled(confirm, transport, payout)` SQL function expresses cancellation across all three statuses, with mutual-exclusion priority Cancelled > Completed > Pending. "Completed" is defined by `payout_session.status = 'Success'` (escrow released), not transport delivery.

**Tech Stack:** Go 1.23 + Restate SDK Go v0.24 + sqlc + golang-migrate + Echo (BE); Next.js 16 + TanStack Query + shadcn/ui (FE). Code generation via `make pgtempl` + `sqlc generate` + `go generate`.

**Spec:** `docs/superpowers/specs/2026-04-30-order-tab-endpoints-design.md`

**Verification model:** This codebase has no biz-handler unit tests. Each task verifies via compile (`go build ./...` + `bun run build`) and where applicable a curl probe against the dev server. End-to-end verification happens at Task 16.

**Out of scope (Khoa drives separately):** the mechanical seller rename (`ListSellerPendingItems → ListSellerIncomingItems`, `ListSellerConfirmed → ListSellerConfirmedOrders` and the matching FE hook + route renames). Plan keeps existing seller names intact so no rename pass is needed mid-flight.

---

## Phase 1 — Schema foundation

### Task 1: Append `order.is_cancelled` SQL function

**Files:**
- Modify: `internal/module/order/db/migrations/0001_init.up.sql` (append at end of file)

- [ ] **Step 1: Append the function**

Add this block at the end of `0001_init.up.sql`:

```sql
-- Order cancellation predicate. Three nullable status columns combine into one
-- boolean: any of confirm/transport/payout in ('Failed', 'Cancelled') means
-- the order is cancelled. NULL inputs (e.g. payout session not yet created,
-- transport row pre-shipment) coerce to FALSE so a missing leg never makes
-- an active order look cancelled. Used by ListBuyer{Pending,Completed,Cancelled}Orders.
CREATE OR REPLACE FUNCTION "order".is_cancelled(
    confirm_status   "order"."status",
    transport_status "order"."status",
    payout_status    "order"."status"
) RETURNS BOOLEAN
LANGUAGE SQL IMMUTABLE
AS $$
    SELECT COALESCE(confirm_status   IN ('Failed', 'Cancelled'), FALSE)
        OR COALESCE(transport_status IN ('Failed', 'Cancelled'), FALSE)
        OR COALESCE(payout_status    IN ('Failed', 'Cancelled'), FALSE);
$$;
```

- [ ] **Step 2: Apply to dev DB (Khoa-driven, ASK FIRST)**

`make migrate` will not re-run an already-applied 0001. Khoa applies via one of:

```bash
# Option A: psql one-shot (fastest, idempotent because of CREATE OR REPLACE)
psql "$DATABASE_URL" -c 'CREATE OR REPLACE FUNCTION "order".is_cancelled(...)' # paste from file

# Option B: golang-migrate force-rollback then re-up
make migrate -- -module order -force 0
make migrate -- -module order
```

Agent: STOP and ask Khoa which option he prefers, or whether he'll apply it himself. Do not run either without confirmation.

- [ ] **Step 3: Verify function exists**

```bash
psql "$DATABASE_URL" -c "SELECT \"order\".is_cancelled('Pending'::\"order\".status, 'Failed'::\"order\".status, NULL);"
```

Expected: `t` (transport=Failed → cancelled).

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/db/migrations/0001_init.up.sql
git commit -m "add order.is_cancelled predicate function"
```

---

## Phase 2 — SQL queries (additive)

### Task 2: Create `order_buyer.sql` with 3 buyer order queries

**Files:**
- Create: `internal/module/order/db/queries/order_buyer.sql`

- [ ] **Step 1: Write the file**

```sql
-- Buyer-side order list queries. Each returns paginated rows with embedded
-- COUNT(*) OVER() so the FE paginator gets total in one round trip. The three
-- queries partition orders into Cancelled > Completed > Pending mutual-exclusion
-- buckets via order.is_cancelled(). Joins payment_session twice (confirm + payout)
-- and transport once.

-- name: ListBuyerPendingOrders :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() AS total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment_session" ps_confirm
       ON ps_confirm."id" = embed_order."confirm_session_id"
LEFT JOIN "order"."payment_session" ps_payout
       ON ps_payout."id" = embed_order."id" AND ps_payout."kind" = 'seller-payout'
LEFT JOIN "order"."transport" t ON t."id" = embed_order."transport_id"
WHERE embed_order."buyer_id" = @buyer_id
  AND NOT "order".is_cancelled(ps_confirm."status", t."status", ps_payout."status")
  AND COALESCE(ps_payout."status", 'Pending'::"order"."status") <> 'Success'
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListBuyerCompletedOrders :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() AS total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment_session" ps_confirm
       ON ps_confirm."id" = embed_order."confirm_session_id"
LEFT JOIN "order"."payment_session" ps_payout
       ON ps_payout."id" = embed_order."id" AND ps_payout."kind" = 'seller-payout'
LEFT JOIN "order"."transport" t ON t."id" = embed_order."transport_id"
WHERE embed_order."buyer_id" = @buyer_id
  AND ps_payout."status" = 'Success'
  AND NOT "order".is_cancelled(ps_confirm."status", t."status", ps_payout."status")
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListBuyerCancelledOrders :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() AS total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment_session" ps_confirm
       ON ps_confirm."id" = embed_order."confirm_session_id"
LEFT JOIN "order"."payment_session" ps_payout
       ON ps_payout."id" = embed_order."id" AND ps_payout."kind" = 'seller-payout'
LEFT JOIN "order"."transport" t ON t."id" = embed_order."transport_id"
WHERE embed_order."buyer_id" = @buyer_id
  AND "order".is_cancelled(ps_confirm."status", t."status", ps_payout."status")
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;
```

- [ ] **Step 2: Run sqlc to verify syntax + generate Go code**

```bash
cd /home/beanbocchi/shopnexus/shopnexus-server
sqlc generate
```

Expected: no errors. Three new generated functions appear in `internal/module/order/db/sqlc/order_buyer.sql.go`: `ListBuyerPendingOrders`, `ListBuyerCompletedOrders`, `ListBuyerCancelledOrders`.

- [ ] **Step 3: Verify generated structs**

```bash
grep -c "func (q \*Queries) ListBuyerPendingOrders\|func (q \*Queries) ListBuyerCompletedOrders\|func (q \*Queries) ListBuyerCancelledOrders" internal/module/order/db/sqlc/order_buyer.sql.go
```

Expected: `3`.

- [ ] **Step 4: Compile check**

```bash
go build ./... 2>&1 | head -20
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/module/order/db/queries/order_buyer.sql internal/module/order/db/sqlc/order_buyer.sql.go internal/module/order/db/sqlc/querier.go internal/module/order/db/sqlc/batch.go internal/module/order/db/sqlc/copyfrom.go
git commit -m "add buyer order list queries split by tab"
```

---

### Task 3: Update `item_custom.sql` — fix `ListBuyerPendingItems` + add `ListBuyerCancelledItems`

**Files:**
- Modify: `internal/module/order/db/queries/item_custom.sql`

- [ ] **Step 1: Replace `ListBuyerPendingItems` and `CountBuyerPendingItems` blocks**

Find:

```sql
-- name: ListBuyerPendingItems :many
SELECT * FROM "order"."item"
WHERE "account_id" = @account_id
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL
ORDER BY "date_created" DESC;

-- name: CountBuyerPendingItems :one
SELECT COUNT(*) FROM "order"."item"
WHERE "account_id" = @account_id
  AND "order_id" IS NULL
  AND "date_cancelled" IS NULL;
```

Replace with:

```sql
-- name: ListBuyerPendingItems :many
-- Pre-confirm items still reachable to the buyer: payment session is either
-- in-flight (Pending) or settled (Success, awaiting seller confirm). Failed /
-- Cancelled sessions are excluded — they belong in ListBuyerCancelledItems.
SELECT i.* FROM "order"."item" i
JOIN "order"."payment_session" ps ON ps."id" = i."payment_session_id"
WHERE i."account_id" = @account_id
  AND i."order_id" IS NULL
  AND i."date_cancelled" IS NULL
  AND ps."status" IN ('Pending', 'Success')
ORDER BY i."date_created" DESC;

-- name: CountBuyerPendingItems :one
SELECT COUNT(*) FROM "order"."item" i
JOIN "order"."payment_session" ps ON ps."id" = i."payment_session_id"
WHERE i."account_id" = @account_id
  AND i."order_id" IS NULL
  AND i."date_cancelled" IS NULL
  AND ps."status" IN ('Pending', 'Success');

-- name: ListBuyerCancelledItems :many
-- Pre-confirm items the buyer can no longer act on: either the checkout
-- failed/was cancelled (session terminal) or the item was individually
-- cancelled (date_cancelled set, e.g. RefundPendingItem on a Success session).
SELECT i.* FROM "order"."item" i
JOIN "order"."payment_session" ps ON ps."id" = i."payment_session_id"
WHERE i."account_id" = @account_id
  AND i."order_id" IS NULL
  AND (ps."status" IN ('Failed', 'Cancelled') OR i."date_cancelled" IS NOT NULL)
ORDER BY i."date_created" DESC;

-- name: CountBuyerCancelledItems :one
SELECT COUNT(*) FROM "order"."item" i
JOIN "order"."payment_session" ps ON ps."id" = i."payment_session_id"
WHERE i."account_id" = @account_id
  AND i."order_id" IS NULL
  AND (ps."status" IN ('Failed', 'Cancelled') OR i."date_cancelled" IS NOT NULL);
```

- [ ] **Step 2: Generate**

```bash
sqlc generate
```

- [ ] **Step 3: Verify generated functions**

```bash
grep -c "ListBuyerPendingItems\|CountBuyerPendingItems\|ListBuyerCancelledItems\|CountBuyerCancelledItems" internal/module/order/db/sqlc/item_custom.sql.go
```

Expected: `4` (one per function definition).

- [ ] **Step 4: Compile**

```bash
go build ./... 2>&1 | head -20
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/module/order/db/queries/item_custom.sql internal/module/order/db/sqlc/item_custom.sql.go internal/module/order/db/sqlc/querier.go
git commit -m "filter pending items by session status and add cancelled items query"
```

---

### Task 4: Replace `GetPendingPayoutSessionForOrder` with `GetPayoutSessionForOrder`

**Files:**
- Modify: `internal/module/order/db/queries/payment_session.sql`

- [ ] **Step 1: Replace the query block**

Find:

```sql
-- name: GetPendingPayoutSessionForOrder :one
SELECT s.* FROM "order"."payment_session" s
WHERE s."kind" = 'seller-payout'
  AND s."status" = 'Pending'
  AND s."to_id" = (SELECT o."seller_id" FROM "order"."order" o WHERE o."id" = @order_id)
LIMIT 1;
```

Replace with:

```sql
-- name: GetPayoutSessionForOrder :one
-- PayoutWorkflow sets payment_session.id = order.id for the seller-payout
-- session (workflow_payout.go:51, sessionID = restate.Key(ctx) = orderID).
-- Returns the row regardless of status so callers can render "Funds released"
-- when status='Success'. Returns sql.ErrNoRows if no payout has started.
SELECT s.* FROM "order"."payment_session" s
WHERE s."id" = @order_id AND s."kind" = 'seller-payout'
LIMIT 1;
```

- [ ] **Step 2: Generate + check old name is gone**

```bash
sqlc generate
grep -c "GetPendingPayoutSessionForOrder\|GetPayoutSessionForOrder" internal/module/order/db/sqlc/payment_session.sql.go
```

Expected: `1` (only `GetPayoutSessionForOrder` remains).

- [ ] **Step 3: Compile (will fail — caller in order.go uses old name)**

```bash
go build ./... 2>&1 | head -10
```

Expected: error pointing at `internal/module/order/biz/order.go` referencing `GetPendingPayoutSessionForOrder`. **Do NOT fix yet** — Task 6 rewrites this caller. Leave the build broken; the next commit fixes it.

- [ ] **Step 4: Commit (broken-build commit, intentional — Task 6 fixes)**

```bash
git add internal/module/order/db/queries/payment_session.sql internal/module/order/db/sqlc/payment_session.sql.go internal/module/order/db/sqlc/querier.go
git commit -m "rewrite payout session lookup by order id"
```

---

## Phase 3 — BE handlers (additive)

### Task 5: Extract `hydrateOrders` into `order_hydrate.go`

**Files:**
- Create: `internal/module/order/biz/order_hydrate.go`
- Modify: `internal/module/order/biz/order.go` (remove the moved code)

- [ ] **Step 1: Move `hydrateOrders`, `mapTransport`, and use new `GetPayoutSessionForOrder`**

Cut the `hydrateOrders` function and `mapTransport` from `order.go`. Paste into a new `order_hydrate.go`. Update the payout-session loader to use the new query and accept any status:

```go
package orderbiz

import (
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

// hydrateOrders fans out one DB pull for items + transports per page, enriches
// items with product/resource data, then enriches each order with its confirm +
// payout session and total amount. The payout session loads regardless of
// status so the FE can render "Funds released" once it reaches Success.
func (b *OrderHandler) hydrateOrders(ctx restate.Context, orders []orderdb.OrderOrder) ([]ordermodel.Order, error) {
	if len(orders) == 0 {
		return []ordermodel.Order{}, nil
	}

	orderIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) uuid.UUID { return o.ID })
	transportIDs := lo.Uniq(lo.Map(orders, func(o orderdb.OrderOrder, _ int) int64 { return o.TransportID }))

	type dbResults struct {
		OrderItems []orderdb.OrderItem      `json:"order_items"`
		Transports []orderdb.OrderTransport `json:"transports"`
	}
	dbData, err := restate.Run(ctx, func(ctx restate.RunContext) (dbResults, error) {
		orderItems, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			OrderID: lo.Map(orderIDs, func(id uuid.UUID, _ int) uuid.NullUUID {
				return uuid.NullUUID{UUID: id, Valid: true}
			}),
		})
		if err != nil {
			return dbResults{}, err
		}
		transports, err := b.storage.Querier().ListTransport(ctx, orderdb.ListTransportParams{ID: transportIDs})
		if err != nil {
			return dbResults{}, err
		}
		return dbResults{OrderItems: orderItems, Transports: transports}, nil
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db fetch order data", err)
	}

	allEnriched, err := b.enrichItems(dbData.OrderItems)
	if err != nil {
		return nil, sharedmodel.WrapErr("enrich order items", err)
	}

	enrichedItemsMap := make(map[uuid.UUID][]ordermodel.OrderItem)
	for _, item := range allEnriched {
		if item.OrderID.Valid {
			enrichedItemsMap[item.OrderID.UUID] = append(enrichedItemsMap[item.OrderID.UUID], item)
		}
	}

	transportMap := lo.KeyBy(dbData.Transports, func(t orderdb.OrderTransport) int64 { return t.ID })

	result := make([]ordermodel.Order, 0, len(orders))
	for _, o := range orders {
		base := mapOrder(o)
		if t, ok := transportMap[o.TransportID]; ok {
			tr := mapTransport(t)
			base.Transport = &tr
		}
		base.Items = enrichedItemsMap[o.ID]

		type orderEnrich struct {
			ConfirmSession orderdb.OrderPaymentSession  `json:"confirm_session"`
			PayoutSession  *orderdb.OrderPaymentSession `json:"payout_session,omitempty"`
			TotalAmount    int64                        `json:"total_amount"`
		}
		orderID := o.ID
		confirmSessionID := o.ConfirmSessionID
		enriched, err := restate.Run(ctx, func(ctx restate.RunContext) (orderEnrich, error) {
			confirmSession, err := b.storage.Querier().GetPaymentSession(ctx, uuid.NullUUID{UUID: confirmSessionID, Valid: true})
			if err != nil {
				return orderEnrich{}, sharedmodel.WrapErr("get confirm session", err)
			}
			res := orderEnrich{ConfirmSession: confirmSession}
			if payoutSession, perr := b.storage.Querier().GetPayoutSessionForOrder(ctx, orderID); perr == nil {
				res.PayoutSession = &payoutSession
			}
			total, err := b.storage.Querier().SumTotalAmountByOrder(ctx, uuid.NullUUID{UUID: orderID, Valid: true})
			if err != nil {
				return orderEnrich{}, sharedmodel.WrapErr("sum paid amount by order", err)
			}
			res.TotalAmount = total
			return res, nil
		})
		if err != nil {
			return nil, sharedmodel.WrapErr("enrich order sessions", err)
		}

		base.TotalAmount = enriched.TotalAmount
		confirmMapped := mapPaymentSession(enriched.ConfirmSession)
		base.ConfirmSession = &confirmMapped
		if enriched.PayoutSession != nil {
			payoutMapped := mapPaymentSession(*enriched.PayoutSession)
			base.PayoutSession = &payoutMapped
		}

		result = append(result, base)
	}

	return result, nil
}

func mapTransport(t orderdb.OrderTransport) ordermodel.Transport {
	return ordermodel.Transport{
		ID:          t.ID,
		OptionID:    t.Option,
		Status:      t.Status,
		Data:        t.Data,
		DateCreated: t.DateCreated,
	}
}
```

Then delete `hydrateOrders` and `mapTransport` from `order.go` (lines 132–248 of the existing file).

- [ ] **Step 2: Compile**

```bash
go build ./... 2>&1 | head -10
```

Expected: clean (this fixes the broken build from Task 4).

- [ ] **Step 3: Commit**

```bash
git add internal/module/order/biz/order_hydrate.go internal/module/order/biz/order.go
git commit -m "extract hydrateOrders and load payout session by order id"
```

---

### Task 6: Create `order_buyer.go` with 5 buyer list methods

**Files:**
- Create: `internal/module/order/biz/order_buyer.go`
- Modify: `internal/module/order/biz/interface.go` (add params types + interface methods)

- [ ] **Step 1: Add params types to `interface.go`**

In `internal/module/order/biz/interface.go`, find the existing `ListBuyerConfirmedParams` struct and add these new param types nearby:

```go
type ListBuyerPendingOrdersParams struct {
	BuyerID uuid.UUID `json:"buyer_id" validate:"required"`
	sharedmodel.PaginationParams
}

type ListBuyerCompletedOrdersParams struct {
	BuyerID uuid.UUID `json:"buyer_id" validate:"required"`
	sharedmodel.PaginationParams
}

type ListBuyerCancelledOrdersParams struct {
	BuyerID uuid.UUID `json:"buyer_id" validate:"required"`
	sharedmodel.PaginationParams
}

type ListBuyerCancelledItemsParams struct {
	AccountID uuid.UUID `json:"account_id" validate:"required"`
	sharedmodel.PaginationParams
}
```

In the `OrderBiz` interface declaration, add the four new method signatures alongside the existing `ListBuyerPendingItems` and `ListBuyerConfirmed`:

```go
ListBuyerPendingOrders(ctx context.Context, params ListBuyerPendingOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
ListBuyerCompletedOrders(ctx context.Context, params ListBuyerCompletedOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
ListBuyerCancelledOrders(ctx context.Context, params ListBuyerCancelledOrdersParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
ListBuyerCancelledItems(ctx context.Context, params ListBuyerCancelledItemsParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
```

- [ ] **Step 2: Write `order_buyer.go`**

Create `internal/module/order/biz/order_buyer.go`:

```go
package orderbiz

import (
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// ListBuyerPendingOrders returns orders that are post-confirm but neither
// completed (payout released) nor cancelled. Includes orders awaiting
// shipment, in transit, delivered-but-not-paid-out.
func (b *OrderHandler) ListBuyerPendingOrders(
	ctx restate.Context,
	params ListBuyerPendingOrdersParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	return b.listBuyerOrders(ctx, params.PaginationParams, params.BuyerID, func(rctx restate.RunContext, p orderListPage) ([]orderdb.OrderOrder, int64, error) {
		rows, err := b.storage.Querier().ListBuyerPendingOrders(rctx, orderdb.ListBuyerPendingOrdersParams{
			BuyerID: p.BuyerID,
			Limit:   p.Limit,
			Offset:  p.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		orders := lo.Map(rows, func(r orderdb.ListBuyerPendingOrdersRow, _ int) orderdb.OrderOrder { return r.OrderOrder })
		var total int64
		if len(rows) > 0 {
			total = rows[0].TotalCount
		}
		return orders, total, nil
	})
}

// ListBuyerCompletedOrders returns orders whose seller payout has been
// released (escrow done). Delivered-but-not-paid-out orders stay Pending.
func (b *OrderHandler) ListBuyerCompletedOrders(
	ctx restate.Context,
	params ListBuyerCompletedOrdersParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	return b.listBuyerOrders(ctx, params.PaginationParams, params.BuyerID, func(rctx restate.RunContext, p orderListPage) ([]orderdb.OrderOrder, int64, error) {
		rows, err := b.storage.Querier().ListBuyerCompletedOrders(rctx, orderdb.ListBuyerCompletedOrdersParams{
			BuyerID: p.BuyerID,
			Limit:   p.Limit,
			Offset:  p.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		orders := lo.Map(rows, func(r orderdb.ListBuyerCompletedOrdersRow, _ int) orderdb.OrderOrder { return r.OrderOrder })
		var total int64
		if len(rows) > 0 {
			total = rows[0].TotalCount
		}
		return orders, total, nil
	})
}

// ListBuyerCancelledOrders returns orders where any of confirm/transport/payout
// is in a Failed or Cancelled state.
func (b *OrderHandler) ListBuyerCancelledOrders(
	ctx restate.Context,
	params ListBuyerCancelledOrdersParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	return b.listBuyerOrders(ctx, params.PaginationParams, params.BuyerID, func(rctx restate.RunContext, p orderListPage) ([]orderdb.OrderOrder, int64, error) {
		rows, err := b.storage.Querier().ListBuyerCancelledOrders(rctx, orderdb.ListBuyerCancelledOrdersParams{
			BuyerID: p.BuyerID,
			Limit:   p.Limit,
			Offset:  p.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		orders := lo.Map(rows, func(r orderdb.ListBuyerCancelledOrdersRow, _ int) orderdb.OrderOrder { return r.OrderOrder })
		var total int64
		if len(rows) > 0 {
			total = rows[0].TotalCount
		}
		return orders, total, nil
	})
}

// ListBuyerCancelledItems returns pre-confirm items that died before becoming
// orders: failed/cancelled checkout sessions, or individually-refunded items
// from a Success session (date_cancelled set).
func (b *OrderHandler) ListBuyerCancelledItems(
	ctx restate.Context,
	params ListBuyerCancelledItemsParams,
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list cancelled items", err)
	}
	return b.listBuyerItems(ctx, params.PaginationParams, params.AccountID,
		func(rctx restate.RunContext, accountID uuid.UUID) ([]orderdb.OrderItem, int64, error) {
			items, err := b.storage.Querier().ListBuyerCancelledItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			total, err := b.storage.Querier().CountBuyerCancelledItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			return items, total, nil
		})
}

// orderListPage carries the per-page args into the per-query closure.
type orderListPage struct {
	BuyerID uuid.UUID
	Limit   null.Int
	Offset  null.Int
}

// listBuyerOrders is the shared backbone for the three order-list endpoints:
// validate → run query in restate.Run → hydrate → wrap in PaginateResult.
func (b *OrderHandler) listBuyerOrders(
	ctx restate.Context,
	pagination sharedmodel.PaginationParams,
	buyerID uuid.UUID,
	fetch func(restate.RunContext, orderListPage) ([]orderdb.OrderOrder, int64, error),
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]
	if err := validator.Validate(struct {
		BuyerID uuid.UUID `validate:"required"`
	}{BuyerID: buyerID}); err != nil {
		return zero, sharedmodel.WrapErr("validate list orders", err)
	}

	type queryResult struct {
		Orders []orderdb.OrderOrder `json:"orders"`
		Total  int64                `json:"total"`
	}
	res, err := restate.Run(ctx, func(rctx restate.RunContext) (queryResult, error) {
		orders, total, err := fetch(rctx, orderListPage{
			BuyerID: buyerID,
			Limit:   null.IntFrom(pagination.Limit),
			Offset:  null.IntFrom(pagination.Offset()),
		})
		if err != nil {
			return queryResult{}, err
		}
		return queryResult{Orders: orders, Total: total}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list orders", err)
	}

	data, err := b.hydrateOrders(ctx, res.Orders)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate orders", err)
	}

	var total null.Int64
	total.SetValid(res.Total)
	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: pagination,
		Total:      total,
		Data:       data,
	}, nil
}

// listBuyerItems is the shared backbone for buyer item-list endpoints.
// Mirrors the existing ListBuyerPendingItems shape including session attach.
func (b *OrderHandler) listBuyerItems(
	ctx restate.Context,
	pagination sharedmodel.PaginationParams,
	accountID uuid.UUID,
	fetch func(restate.RunContext, uuid.UUID) ([]orderdb.OrderItem, int64, error),
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	type pageResult struct {
		Items []orderdb.OrderItem `json:"items"`
		Total int64               `json:"total"`
	}
	res, err := restate.Run(ctx, func(rctx restate.RunContext) (pageResult, error) {
		items, total, err := fetch(rctx, accountID)
		if err != nil {
			return pageResult{}, err
		}
		return pageResult{Items: items, Total: total}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list items", err)
	}

	enriched, err := b.enrichItems(res.Items)
	if err != nil {
		return zero, sharedmodel.WrapErr("enrich items", err)
	}

	if len(enriched) > 0 {
		sessionIDs := lo.Uniq(lo.Map(enriched, func(it ordermodel.OrderItem, _ int) uuid.UUID { return it.PaymentSessionID }))
		var sessions []orderdb.OrderPaymentSession
		sessions, err = restate.Run(ctx, func(rctx restate.RunContext) ([]orderdb.OrderPaymentSession, error) {
			return b.storage.Querier().ListPaymentSession(rctx, orderdb.ListPaymentSessionParams{ID: sessionIDs})
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("db fetch payment sessions", err)
		}
		sessionMap := lo.KeyBy(sessions, func(s orderdb.OrderPaymentSession) uuid.UUID { return s.ID })
		for i := range enriched {
			if s, ok := sessionMap[enriched[i].PaymentSessionID]; ok {
				mapped := mapPaymentSession(s)
				enriched[i].PaymentSession = &mapped
			}
		}
	}

	var totalVal null.Int64
	totalVal.SetValid(res.Total)
	return sharedmodel.PaginateResult[ordermodel.OrderItem]{
		PageParams: pagination,
		Total:      totalVal,
		Data:       enriched,
	}, nil
}
```

- [ ] **Step 3: Refactor `ListBuyerPendingItems` in `pending_buyer.go` to use `listBuyerItems`**

In `internal/module/order/biz/pending_buyer.go`, replace the existing `ListBuyerPendingItems` body (lines ~26–90, the function down to the closing `}` before `CancelBuyerPending`) with:

```go
func (b *OrderHandler) ListBuyerPendingItems(
	ctx restate.Context,
	params ListBuyerPendingItemsParams,
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.PaginateResult[ordermodel.OrderItem]{}, sharedmodel.WrapErr("validate list pending items", err)
	}
	return b.listBuyerItems(ctx, params.PaginationParams, params.AccountID,
		func(rctx restate.RunContext, accountID uuid.UUID) ([]orderdb.OrderItem, int64, error) {
			items, err := b.storage.Querier().ListBuyerPendingItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			total, err := b.storage.Querier().CountBuyerPendingItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			return items, total, nil
		})
}
```

Drop the now-unused imports from `pending_buyer.go` if any (likely `lo` if no other usage in that file).

- [ ] **Step 4: Compile**

```bash
go build ./... 2>&1 | head -10
```

Expected: clean.

- [ ] **Step 5: Regenerate restate proxy**

```bash
go generate ./internal/module/order/biz/
```

Expected: `restate_gen.go` now contains 4 new methods. Verify:

```bash
grep -c "ListBuyerPendingOrders\|ListBuyerCompletedOrders\|ListBuyerCancelledOrders\|ListBuyerCancelledItems" internal/module/order/biz/restate_gen.go
```

Expected: ≥ `8` (4 method names × 2 — declaration + dispatch line per method).

- [ ] **Step 6: Compile again**

```bash
go build ./... 2>&1 | head -10
```

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/module/order/biz/order_buyer.go internal/module/order/biz/interface.go internal/module/order/biz/pending_buyer.go internal/module/order/biz/restate_gen.go
git commit -m "add buyer order list endpoints split by tab"
```

---

### Task 7: Wire HTTP routes for the 4 new buyer endpoints

**Files:**
- Modify: `internal/module/order/transport/echo/order.go`

- [ ] **Step 1: Add 4 route registrations**

In `NewHandler` (around line 50-65 where buyer routes are registered), find the `// Buyer - Pending` block and the `// Buyer - Confirmed` block. Replace both with:

```go
// Buyer - Pending
g.POST("/buyer/checkout", h.BuyerCheckout, rlCheckout)
g.POST("/buyer/checkout/:sessionID/cancel", h.CancelBuyerCheckout)
g.POST("/buyer/checkout/:sessionID/payment-url", h.EnsureBuyerCheckoutPaymentURL, rlCheckout)
g.GET("/buyer/pending-items", h.ListBuyerPendingItems)
g.GET("/buyer/pending-orders", h.ListBuyerPendingOrders)
g.DELETE("/buyer/pending-items/:id", h.CancelBuyerPending)

// Buyer - Completed
g.GET("/buyer/completed-orders", h.ListBuyerCompletedOrders)

// Buyer - Cancelled
g.GET("/buyer/cancelled-items", h.ListBuyerCancelledItems)
g.GET("/buyer/cancelled-orders", h.ListBuyerCancelledOrders)

// Buyer - Order detail
g.GET("/buyer/orders/:id", h.GetBuyerOrder)
```

(Routes `/buyer/pending` and `/buyer/confirmed` go away — the FE has not adopted yet but Phase 4 will replace them. The old `ListBuyerConfirmed` BE handler still exists but its route is no longer reachable.)

- [ ] **Step 2: Add the 4 new handler functions to `order.go`**

Append at the end of `internal/module/order/transport/echo/order.go`:

```go
// --- Buyer Pending Orders ---

type ListBuyerPendingOrdersRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerPendingOrders(c echo.Context) error {
	var req ListBuyerPendingOrdersRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}
	result, err := h.biz.ListBuyerPendingOrders(c.Request().Context(), orderbiz.ListBuyerPendingOrdersParams{
		BuyerID:          claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromPaginate(c.Response().Writer, result)
}

// --- Buyer Completed Orders ---

type ListBuyerCompletedOrdersRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerCompletedOrders(c echo.Context) error {
	var req ListBuyerCompletedOrdersRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}
	result, err := h.biz.ListBuyerCompletedOrders(c.Request().Context(), orderbiz.ListBuyerCompletedOrdersParams{
		BuyerID:          claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromPaginate(c.Response().Writer, result)
}

// --- Buyer Cancelled Orders ---

type ListBuyerCancelledOrdersRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerCancelledOrders(c echo.Context) error {
	var req ListBuyerCancelledOrdersRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}
	result, err := h.biz.ListBuyerCancelledOrders(c.Request().Context(), orderbiz.ListBuyerCancelledOrdersParams{
		BuyerID:          claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromPaginate(c.Response().Writer, result)
}

// --- Buyer Cancelled Items ---

type ListBuyerCancelledItemsRequest struct {
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerCancelledItems(c echo.Context) error {
	var req ListBuyerCancelledItemsRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}
	result, err := h.biz.ListBuyerCancelledItems(c.Request().Context(), orderbiz.ListBuyerCancelledItemsParams{
		AccountID:        claims.Account.ID,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromPaginate(c.Response().Writer, result)
}
```

- [ ] **Step 3: Compile**

```bash
go build ./... 2>&1 | head -10
```

Expected: clean.

- [ ] **Step 4: Run dev server briefly + curl probe (Khoa to provide auth token)**

```bash
make build && ./bin/server &
sleep 3
curl -sf -H "Authorization: Bearer $DEV_TOKEN" \
  "http://localhost:8080/api/v1/order/buyer/pending-orders?limit=5&offset=0" | head -c 500
```

Expected: JSON envelope `{"data":[...],"total":...}`. If 401, Khoa generates a token. If 500, paste error.

- [ ] **Step 5: Stop server, commit**

```bash
kill %1 2>/dev/null
git add internal/module/order/transport/echo/order.go
git commit -m "wire buyer pending-orders, completed-orders, cancelled-items, cancelled-orders routes"
```

---

## Phase 4 — FE adopt

### Task 8: Add 4 new buyer hooks to `order.buyer.ts`

**Files:**
- Modify: `../shopnexus-website-v3/core/order/order.buyer.ts`

- [ ] **Step 1: Add hooks**

Append after the existing `useListBuyerConfirmed` definition (around line 193):

```typescript
export const useListBuyerPendingOrders = (params: PaginationParams) =>
  useInfiniteQueryPagination<TOrder>(
    ['order', 'buyer', 'pending-orders'],
    'order/buyer/pending-orders',
    params,
  )

export const useListBuyerCompletedOrders = (params: PaginationParams) =>
  useInfiniteQueryPagination<TOrder>(
    ['order', 'buyer', 'completed-orders'],
    'order/buyer/completed-orders',
    params,
  )

export const useListBuyerCancelledOrders = (params: PaginationParams) =>
  useInfiniteQueryPagination<TOrder>(
    ['order', 'buyer', 'cancelled-orders'],
    'order/buyer/cancelled-orders',
    params,
  )

export const useListBuyerCancelledItems = (params: PaginationParams) =>
  useInfiniteQueryPagination<TOrderItem>(
    ['order', 'buyer', 'cancelled-items'],
    'order/buyer/cancelled-items',
    params,
  )
```

Update the existing `useListBuyerPendingItems` URL — current `'order/buyer/pending'` becomes `'order/buyer/pending-items'` to match new routes:

Find:
```typescript
export const useListBuyerPendingItems = (params: PaginationParams) =>
  useInfiniteQueryPagination<TOrderItem>(
    ['order', 'buyer', 'pending-items'],
    'order/buyer/pending',
    params
  )
```

Replace with:
```typescript
export const useListBuyerPendingItems = (params: PaginationParams) =>
  useInfiniteQueryPagination<TOrderItem>(
    ['order', 'buyer', 'pending-items'],
    'order/buyer/pending-items',
    params,
  )
```

Update the cancel mutation URL:
Find: `customFetchStandard<void>(`order/buyer/pending/${id}`,`
Replace: `customFetchStandard<void>(`order/buyer/pending-items/${id}`,`

Update `useGetBuyerOrder` URL:
Find: `customFetchStandard<TOrder>(`order/buyer/confirmed/${id}`)`
Replace: `customFetchStandard<TOrder>(`order/buyer/orders/${id}`)`

Update the cancel mutation's `onSuccess` invalidation to also invalidate cancelled-items so a cancellation immediately moves the item to the right tab:

Find:
```typescript
onSuccess: async () => {
  await qc.invalidateQueries({ queryKey: ['order', 'buyer', 'pending-items'] })
},
```

Replace:
```typescript
onSuccess: async () => {
  await Promise.all([
    qc.invalidateQueries({ queryKey: ['order', 'buyer', 'pending-items'] }),
    qc.invalidateQueries({ queryKey: ['order', 'buyer', 'cancelled-items'] }),
    qc.invalidateQueries({ queryKey: ['order', 'buyer', 'pending-orders'] }),
    qc.invalidateQueries({ queryKey: ['order', 'buyer', 'cancelled-orders'] }),
  ])
},
```

Delete the old `useListBuyerConfirmed` hook block (no longer used after page.tsx refactor in Task 10).

Delete the `useGetBuyerOverview` block — it's only used by the obsolete pending tab logic and references the deleted `useListBuyerConfirmed`.

- [ ] **Step 2: Type-check**

```bash
cd ../shopnexus-website-v3
bun run typecheck 2>&1 | head -20
```

Expected: errors only at `app/(marketplace)/account/orders/page.tsx` referencing `useListBuyerConfirmed` / `useGetBuyerOverview`. Task 10 fixes those.

- [ ] **Step 3: Commit (broken-build commit, intentional — Task 10 fixes)**

```bash
cd ../shopnexus-website-v3
git add core/order/order.buyer.ts
git commit -m "add buyer pending/completed/cancelled order+item hooks"
```

---

### Task 9: Extract `PendingItemCard` and create `ItemList` component

**Files:**
- Create: `../shopnexus-website-v3/app/(marketplace)/account/orders/_components/pending-item-card.tsx`
- Create: `../shopnexus-website-v3/app/(marketplace)/account/orders/_components/item-list.tsx`

- [ ] **Step 1: Create `pending-item-card.tsx`**

Extract `PendingItemCard` from `app/(marketplace)/account/orders/page.tsx` (current lines ~38–119) into its own file, with the cancel-button gating fix applied (defense-in-depth — won't reach this card after BE filter, but matches `CancelBuyerPending`'s allowed branches):

```tsx
"use client"

import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Package, Clock } from "lucide-react"
import { cn } from "@/lib/utils"
import { useExchangeRates, useCurrency } from "@/core/common/currency"
import { formatPriceInline } from "@/lib/money"
import { useEnsureBuyerPaymentURL, TOrderItem } from "@/core/order/order.buyer"
import { toast } from "sonner"

type Props = {
  item: TOrderItem
  onCancel?: (id: number) => void
  readOnly?: boolean
}

export function PendingItemCard({ item, onCancel, readOnly = false }: Props) {
  const preferred = useCurrency()
  const { data: rateData } = useExchangeRates()
  const fmt = (amount: number) =>
    formatPriceInline(amount, "VND", preferred, rateData?.rates, "native")

  const txStatus = item.payment_session?.status
  let badgeLabel: string
  let badgeColor: string
  if (txStatus === "Pending") {
    badgeLabel = "Awaiting Payment"
    badgeColor = "bg-yellow-100 text-yellow-800"
  } else if (txStatus === "Failed") {
    badgeLabel = "Payment Failed"
    badgeColor = "bg-red-100 text-red-800"
  } else if (txStatus === "Cancelled") {
    badgeLabel = "Cancelled"
    badgeColor = "bg-gray-100 text-gray-800"
  } else {
    badgeLabel = "Awaiting Seller"
    badgeColor = "bg-yellow-100 text-yellow-800"
  }

  const ensurePaymentURL = useEnsureBuyerPaymentURL()
  const handleContinuePayment = async () => {
    const sessionID = item.payment_session?.id
    if (!sessionID) {
      toast.error("Payment session not found")
      return
    }
    try {
      const { payment_url } = await ensurePaymentURL.mutateAsync(String(sessionID))
      if (payment_url) {
        window.location.href = payment_url
        return
      }
      toast.error("Payment URL unavailable")
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Could not start payment"
      toast.error(msg)
    }
  }

  const isTerminal = txStatus === "Failed" || txStatus === "Cancelled"
  const showCancel = !readOnly && !item.order_id && !item.date_cancelled && !isTerminal && onCancel

  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center gap-3">
          <div className="relative h-16 w-16 rounded bg-muted flex items-center justify-center flex-shrink-0">
            <Package className="h-6 w-6 text-muted-foreground" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="font-medium truncate">{item.sku_name}</p>
            <p className="text-sm text-muted-foreground inline-flex items-center gap-1">
              Qty: {item.quantity} &middot; {fmt(item.subtotal_amount)} total
            </p>
            {item.note && (
              <p className="text-sm text-muted-foreground truncate">{item.note}</p>
            )}
          </div>
          <div className="flex flex-col items-end gap-2 flex-shrink-0">
            <Badge variant="secondary" className={cn("font-normal gap-1", badgeColor)}>
              <Clock className="h-3 w-3" />
              {badgeLabel}
            </Badge>
            <span className="text-sm font-medium">{fmt(item.total_amount)}</span>
            {!readOnly && txStatus === "Pending" && (
              <Button variant="default" size="sm" onClick={handleContinuePayment}>
                Continue Payment
              </Button>
            )}
            {showCancel && (
              <Button
                variant="ghost"
                size="sm"
                className="text-destructive h-7 px-2"
                onClick={() => onCancel(item.id)}
              >
                Cancel
              </Button>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
```

- [ ] **Step 2: Create `item-list.tsx`**

```tsx
"use client"

import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Loader2, Inbox } from "lucide-react"
import { TOrderItem, useCancelBuyerPending } from "@/core/order/order.buyer"
import { PendingItemCard } from "./pending-item-card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { toast } from "sonner"

type Props = {
  items: TOrderItem[]
  hasNextPage?: boolean
  isFetchingNextPage?: boolean
  onLoadMore?: () => void
  cancellable?: boolean
  emptyMessage?: string
}

export function ItemList({
  items,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
  cancellable = false,
  emptyMessage = "No items",
}: Props) {
  const cancelMutation = useCancelBuyerPending()
  const [cancelId, setCancelId] = useState<number | null>(null)

  const handleCancel = async () => {
    if (cancelId === null) return
    try {
      await cancelMutation.mutateAsync(cancelId)
      toast.success("Item cancelled.")
      setCancelId(null)
    } catch {
      toast.error("Failed to cancel item.")
    }
  }

  if (items.length === 0) {
    return (
      <div className="text-center py-8 text-sm text-muted-foreground inline-flex flex-col items-center gap-2 w-full">
        <Inbox className="h-6 w-6" />
        {emptyMessage}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {items.map((item) => (
        <PendingItemCard
          key={item.id}
          item={item}
          onCancel={cancellable ? setCancelId : undefined}
          readOnly={!cancellable}
        />
      ))}
      {hasNextPage && (
        <div className="text-center">
          <Button variant="outline" size="sm" onClick={onLoadMore} disabled={isFetchingNextPage}>
            {isFetchingNextPage ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Loading...
              </>
            ) : (
              "Load More"
            )}
          </Button>
        </div>
      )}

      <Dialog open={cancelId !== null} onOpenChange={(open) => { if (!open) setCancelId(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Cancel Item</DialogTitle>
            <DialogDescription>
              Are you sure you want to cancel this item? This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCancelId(null)}>Keep Item</Button>
            <Button variant="destructive" onClick={handleCancel} disabled={cancelMutation.isPending}>
              {cancelMutation.isPending ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Cancelling...
                </>
              ) : (
                "Cancel Item"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
```

- [ ] **Step 3: Type-check (page.tsx still broken — that's Task 10)**

```bash
bun run typecheck 2>&1 | head -10
```

Expected: errors only in `page.tsx`.

- [ ] **Step 4: Commit**

```bash
git add app/\(marketplace\)/account/orders/_components/pending-item-card.tsx app/\(marketplace\)/account/orders/_components/item-list.tsx
git commit -m "extract pending item card and item list components"
```

---

### Task 10: Refactor buyer `page.tsx` with new tab structure

**Files:**
- Modify: `../shopnexus-website-v3/app/(marketplace)/account/orders/page.tsx`
- Delete: `../shopnexus-website-v3/lib/order-status.ts` (no longer needed — filtering is now BE-side)

- [ ] **Step 1: Replace `page.tsx` entirely**

Overwrite `app/(marketplace)/account/orders/page.tsx` with:

```tsx
"use client"

import { useMemo } from "react"
import Link from "next/link"
import {
  useListBuyerPendingItems,
  useListBuyerPendingOrders,
  useListBuyerCompletedOrders,
  useListBuyerCancelledItems,
  useListBuyerCancelledOrders,
} from "@/core/order/order.buyer"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { OrderList } from "./_components/order-list"
import { ItemList } from "./_components/item-list"
import { Inbox } from "lucide-react"

const PAGE_SIZE = 20

function LoadingSkeleton() {
  return (
    <div className="space-y-4">
      {Array.from({ length: 3 }).map((_, i) => (
        <Card key={i}>
          <CardContent className="p-4">
            <div className="flex items-center gap-3">
              <Skeleton className="h-16 w-16 rounded" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-4 w-48" />
                <Skeleton className="h-3 w-24" />
              </div>
              <Skeleton className="h-6 w-24" />
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function EmptyState({ message, cta }: { message: string; cta?: boolean }) {
  return (
    <div className="text-center py-12">
      <div className="inline-flex h-16 w-16 items-center justify-center rounded-full bg-muted mb-4">
        <Inbox className="h-8 w-8 text-muted-foreground" />
      </div>
      <h3 className="text-lg font-semibold mb-2">{message}</h3>
      {cta && (
        <Button asChild className="mt-4">
          <Link href="/">Start Shopping</Link>
        </Button>
      )}
    </div>
  )
}

function PendingTab() {
  const itemsQ = useListBuyerPendingItems({ limit: PAGE_SIZE })
  const ordersQ = useListBuyerPendingOrders({ limit: PAGE_SIZE })

  const items = useMemo(() => itemsQ.data?.pages.flatMap((p) => p.data) ?? [], [itemsQ.data])
  const orders = useMemo(() => ordersQ.data?.pages.flatMap((p) => p.data) ?? [], [ordersQ.data])

  if (itemsQ.isLoading || ordersQ.isLoading) return <LoadingSkeleton />
  if (items.length === 0 && orders.length === 0) {
    return <EmptyState message="No pending orders" cta />
  }

  return (
    <div className="space-y-6">
      {items.length > 0 && (
        <section className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">Awaiting Payment / Seller Confirm</h3>
          <ItemList
            items={items}
            cancellable
            hasNextPage={itemsQ.hasNextPage}
            isFetchingNextPage={itemsQ.isFetchingNextPage}
            onLoadMore={() => itemsQ.fetchNextPage()}
          />
        </section>
      )}
      {orders.length > 0 && (
        <section className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">Awaiting Delivery & Payout</h3>
          <OrderList
            orders={orders}
            hasNextPage={ordersQ.hasNextPage}
            isFetchingNextPage={ordersQ.isFetchingNextPage}
            onLoadMore={() => ordersQ.fetchNextPage()}
          />
        </section>
      )}
    </div>
  )
}

function CompletedTab() {
  const ordersQ = useListBuyerCompletedOrders({ limit: PAGE_SIZE })
  const orders = useMemo(() => ordersQ.data?.pages.flatMap((p) => p.data) ?? [], [ordersQ.data])

  if (ordersQ.isLoading) return <LoadingSkeleton />
  if (orders.length === 0) return <EmptyState message="No completed orders" />

  return (
    <OrderList
      orders={orders}
      hasNextPage={ordersQ.hasNextPage}
      isFetchingNextPage={ordersQ.isFetchingNextPage}
      onLoadMore={() => ordersQ.fetchNextPage()}
    />
  )
}

function CancelledTab() {
  const itemsQ = useListBuyerCancelledItems({ limit: PAGE_SIZE })
  const ordersQ = useListBuyerCancelledOrders({ limit: PAGE_SIZE })

  const items = useMemo(() => itemsQ.data?.pages.flatMap((p) => p.data) ?? [], [itemsQ.data])
  const orders = useMemo(() => ordersQ.data?.pages.flatMap((p) => p.data) ?? [], [ordersQ.data])

  if (itemsQ.isLoading || ordersQ.isLoading) return <LoadingSkeleton />
  if (items.length === 0 && orders.length === 0) return <EmptyState message="No cancelled orders" />

  return (
    <div className="space-y-6">
      {items.length > 0 && (
        <section className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">Cancelled Items (pre-confirm)</h3>
          <ItemList
            items={items}
            hasNextPage={itemsQ.hasNextPage}
            isFetchingNextPage={itemsQ.isFetchingNextPage}
            onLoadMore={() => itemsQ.fetchNextPage()}
          />
        </section>
      )}
      {orders.length > 0 && (
        <section className="space-y-3">
          <h3 className="text-sm font-medium text-muted-foreground">Cancelled Orders</h3>
          <OrderList
            orders={orders}
            hasNextPage={ordersQ.hasNextPage}
            isFetchingNextPage={ordersQ.isFetchingNextPage}
            onLoadMore={() => ordersQ.fetchNextPage()}
          />
        </section>
      )}
    </div>
  )
}

export default function OrdersPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">My Orders</h1>
        <p className="text-muted-foreground">View and track your orders</p>
      </div>

      <Tabs defaultValue="pending">
        <TabsList>
          <TabsTrigger value="pending">Pending</TabsTrigger>
          <TabsTrigger value="completed">Completed</TabsTrigger>
          <TabsTrigger value="cancelled">Cancelled</TabsTrigger>
        </TabsList>

        <TabsContent value="pending" className="mt-6">
          <PendingTab />
        </TabsContent>

        <TabsContent value="completed" className="mt-6">
          <CompletedTab />
        </TabsContent>

        <TabsContent value="cancelled" className="mt-6">
          <CancelledTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
```

- [ ] **Step 2: Check if `OrderList` component supports the new prop signature**

```bash
grep -n "type Props\|interface Props\|export function OrderList" app/\(marketplace\)/account/orders/_components/order-list.tsx
```

If the existing `OrderList` already accepts `orders / hasNextPage / isFetchingNextPage / onLoadMore`, no change needed. If not, adapt the call sites or extend the component (paste current props signature back here so the next step can adjust).

- [ ] **Step 3: Delete `lib/order-status.ts`**

```bash
git rm lib/order-status.ts
```

(All consumers now use BE-side filtering. If the file is referenced elsewhere — check with `grep -rn "order-status" --include='*.ts' --include='*.tsx'` — fix those imports inline.)

- [ ] **Step 4: Type-check + build**

```bash
bun run typecheck && bun run build 2>&1 | tail -30
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add app/\(marketplace\)/account/orders/page.tsx lib/order-status.ts
git commit -m "rewire buyer orders page to per-tab endpoints"
```

---

## Phase 5 — End-to-end verification

### Task 11: Smoke test the full flow on dev

**Files:** none (verification task)

- [ ] **Step 1: Start BE + FE**

```bash
# Terminal 1
cd /home/beanbocchi/shopnexus/shopnexus-server
make dev

# Terminal 2
cd ../shopnexus-website-v3
bun dev
```

- [ ] **Step 2: Manual flow checklist**

In the buyer UI at `http://localhost:5006/account/orders`:

1. Pending tab loads two sections (Awaiting Payment + Awaiting Delivery) when both have rows; one section when only one has rows.
2. Cancel a pending item → it disappears from Pending and appears in Cancelled tab (Cancelled Items section).
3. Failed-checkout items appear in Cancelled, NOT Pending.
4. Completed tab shows only orders whose payout has been released.
5. Cancel button is hidden on items where `payment_session.status` is Failed or Cancelled.

If any of these fail, capture the network request to the failing endpoint and the rendered output, then debug.

- [ ] **Step 3: Commit (if any patches were needed)**

If patches were needed, commit them with a focused message. If clean, skip.

---

## Phase 6 — Cleanup (BE)

### Task 12: Remove dead buyer methods, queries, and routes

**Files:**
- Modify: `internal/module/order/biz/order.go` (delete `ListBuyerConfirmed` + params type)
- Modify: `internal/module/order/biz/interface.go` (delete `ListBuyerConfirmedParams` + interface method)
- Modify: `internal/module/order/db/queries/order.sql` (delete `ListCountBuyerOrder` + `ListBuyerOrders`)

- [ ] **Step 1: Delete biz method**

In `internal/module/order/biz/order.go`, delete the entire `ListBuyerConfirmed` function (lines 46–86 of the existing file — from `// ListBuyerConfirmed` doc comment through the closing `}` of the function).

- [ ] **Step 2: Delete from interface**

In `interface.go`, remove the `ListBuyerConfirmed(...)` line from the `OrderBiz` interface and the `ListBuyerConfirmedParams` struct.

- [ ] **Step 3: Delete SQL queries**

In `internal/module/order/db/queries/order.sql`, delete the `-- name: ListCountBuyerOrder` and `-- name: ListBuyerOrders` blocks. Keep the seller queries and shared ones (`ListOrdersByTransportID`, `HasPurchasedSku`, `ListSuccessOrdersBySkus`, `ValidateOrderForReview`).

- [ ] **Step 4: Regenerate sqlc + restate**

```bash
sqlc generate
go generate ./internal/module/order/biz/
```

- [ ] **Step 5: Compile**

```bash
go build ./... 2>&1 | head -10
```

Expected: clean. If any consumer of `ListBuyerConfirmed` remains (e.g. dashboard.go), grep for it and remove.

```bash
grep -rn "ListBuyerConfirmed\|ListCountBuyerOrder\|ListBuyerOrders\b" internal/ 2>/dev/null
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add -A internal/module/order/
git commit -m "remove obsolete ListBuyerConfirmed and order list queries"
```

---

## Self-review checklist (post-write)

- [x] Spec coverage: All five buyer endpoints are tasks 2/3/6, hydrate update in task 5, FE refactor in tasks 8–10, cleanup in 12. Seller rename is explicitly out of scope per Khoa's preference for self-driven mechanical renames.
- [x] No placeholders: every code block is complete; commands have expected output where useful.
- [x] Type consistency: `ListBuyerPendingOrdersParams` / `ListBuyerCompletedOrdersParams` / `ListBuyerCancelledOrdersParams` all use `BuyerID`; `ListBuyerCancelledItemsParams` uses `AccountID` matching existing `ListBuyerPendingItemsParams`. SQL function name `order.is_cancelled` consistent throughout.
- Migration apply (Task 1 Step 2) intentionally pauses for Khoa — dev DB workflow is operator-specific.
- Task 4 ends on a broken build (referenced by Task 6) — explicitly flagged so the reader doesn't try to "fix" it on commit.
- Task 8 also ends on broken build (referenced by Task 10) — same flag.

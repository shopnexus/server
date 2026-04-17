# Drop `order.status` Column — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the `order.status` column entirely. Frontend infers display state from `payment.status` + `transport.status`.

**Architecture:** Drop DB column, regenerate SQLC, fix all Go references, add Transport embedding to order response (FE needs it for inference), update FE to derive status badges from payment + transport.

**Tech Stack:** Go, PostgreSQL, SQLC, pgtempl, Echo, Next.js/React, TanStack Query

---

### Task 1: Drop status column from migration + fix custom SQL

**Files:**
- Modify: `internal/module/order/db/migrations/0001_init.up.sql:88`
- Modify: `internal/module/order/db/queries/order.sql`
- Modify: `internal/module/order/db/queries/item_custom.sql:60-88`

- [ ] **Step 1: Remove status column from order table in migration**

In `internal/module/order/db/migrations/0001_init.up.sql`, remove line 88:

```sql
-- REMOVE this line:
    "status" "order"."status" NOT NULL DEFAULT 'Pending',
```

Keep the `order.status` enum definition (line 18) — it's still used by payment, refund, refund_dispute tables.

- [ ] **Step 2: Update order.sql — remove order_status filter, add buyer query**

Replace the entire `internal/module/order/db/queries/order.sql` with:

```sql
-- Custom order queries

-- name: ListCountBuyerOrder :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() as total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment" p ON embed_order."payment_id" = p."id"
WHERE embed_order."buyer_id" = @buyer_id
    AND (p."status" = ANY(sqlc.slice('payment_status')) OR sqlc.slice('payment_status') IS NULL)
    AND (embed_order."id"::text ILIKE '%' || sqlc.narg('search')::text || '%' OR sqlc.narg('search') IS NULL)
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountSellerOrder :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() as total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment" p ON embed_order."payment_id" = p."id"
WHERE embed_order."seller_id" = @seller_id
    AND (p."status" = ANY(sqlc.slice('payment_status')) OR sqlc.slice('payment_status') IS NULL)
    AND (embed_order."id"::text ILIKE '%' || sqlc.narg('search')::text || '%' OR sqlc.narg('search') IS NULL)
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: SetOrderPayment :exec
UPDATE "order"."order"
SET "payment_id" = @payment_id
WHERE "id" = ANY(@ids::uuid[]) AND "buyer_id" = @buyer_id AND "payment_id" IS NULL;
```

- [ ] **Step 3: Update item_custom.sql — replace o.status = 'Success' with payment join**

In `internal/module/order/db/queries/item_custom.sql`, update three queries:

**HasPurchasedSku** (lines 60-68) — replace with:
```sql
-- name: HasPurchasedSku :one
SELECT EXISTS(
    SELECT 1
    FROM "order"."item" i
    JOIN "order"."order" o ON i."order_id" = o."id"
    JOIN "order"."payment" p ON o."payment_id" = p."id"
    WHERE i."account_id" = @account_id
      AND i."sku_id" = ANY(@sku_ids::uuid[])
      AND p."status" = 'Success'
) AS has_purchased;
```

**ListSuccessOrdersBySkus** (lines 70-77) — replace with:
```sql
-- name: ListSuccessOrdersBySkus :many
SELECT DISTINCT o.id, o.buyer_id, o.seller_id, o.payment_id, o.transport_id, o.confirmed_by_id, o.address, o.product_cost, o.product_discount, o.transport_cost, o.total, o.note, o.data, o.date_created
FROM "order"."order" o
JOIN "order"."item" i ON i."order_id" = o."id"
JOIN "order"."payment" p ON o."payment_id" = p."id"
WHERE o."buyer_id" = @buyer_id
  AND i."sku_id" = ANY(@sku_ids::uuid[])
  AND p."status" = 'Success'
ORDER BY o."date_created" DESC;
```

Note: removed `o.status` from the SELECT column list.

**ValidateOrderForReview** (lines 79-88) — replace with:
```sql
-- name: ValidateOrderForReview :one
SELECT EXISTS(
    SELECT 1
    FROM "order"."order" o
    JOIN "order"."item" i ON i."order_id" = o."id"
    JOIN "order"."payment" p ON o."payment_id" = p."id"
    WHERE o."id" = @order_id
      AND o."buyer_id" = @buyer_id
      AND i."sku_id" = ANY(@sku_ids::uuid[])
      AND p."status" = 'Success'
) AS is_valid;
```

- [ ] **Step 4: Regenerate pgtempl + SQLC**

```bash
go run ./cmd/pgtempl/ -module order -skip-schema-prefix -single-file=generated_queries.sql
sqlc generate
```

Verify: `grep -r "\"status\"" internal/module/order/db/sqlc/models.go` should NOT show a Status field on `OrderOrder` struct. Payment and other tables should still have status.

- [ ] **Step 5: Commit**

```bash
git add internal/module/order/db/
git commit -m "drop order.status column, update queries to use payment.status"
```

---

### Task 2: Update backend model + interface

**Files:**
- Modify: `internal/module/order/model/order.go:40-56`
- Modify: `internal/module/order/biz/interface.go:47,175-192`

- [ ] **Step 1: Add Transport struct and remove Status from Order model**

In `internal/module/order/model/order.go`:

Add a Transport struct after the Payment struct:

```go
type Transport struct {
	ID          uuid.UUID                    `json:"id"`
	Option      string                       `json:"option"`
	Status      orderdb.OrderTransportStatus `json:"status"`
	Cost        sharedmodel.Concurrency      `json:"cost"`
	Data        json.RawMessage              `json:"data"`
	DateCreated time.Time                    `json:"date_created"`
}
```

Update the Order struct — remove `Status` field, replace `TransportID` with `Transport`:

```go
type Order struct {
	ID              uuid.UUID               `json:"id"`
	BuyerID         uuid.UUID               `json:"buyer_id"`
	SellerID        uuid.UUID               `json:"seller_id"`
	Transport       *Transport              `json:"transport"`
	Payment         *Payment                `json:"payment"`
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
```

- [ ] **Step 2: Update interface params — remove Status, CancelBuyerOrder**

In `internal/module/order/biz/interface.go`:

Remove line 47 from the interface:
```go
// DELETE: CancelBuyerOrder(ctx context.Context, params CancelBuyerOrderParams) error
```

Update `ListBuyerConfirmedParams` (lines 175-179) — remove `Status`, add `BuyerID` and `PaymentStatus`:
```go
type ListBuyerConfirmedParams struct {
	BuyerID       uuid.UUID             `validate:"required"`
	PaymentStatus []orderdb.OrderStatus `validate:"omitempty"`
	sharedmodel.PaginationParams
}
```

Update `ListSellerConfirmedParams` (lines 181-187) — remove `OrderStatus`:
```go
type ListSellerConfirmedParams struct {
	SellerID      uuid.UUID             `validate:"required"`
	Search        null.String           `validate:"omitnil"`
	PaymentStatus []orderdb.OrderStatus `validate:"omitempty"`
	sharedmodel.PaginationParams
}
```

Delete `CancelBuyerOrderParams` (lines 189-192).

- [ ] **Step 3: Regenerate Restate proxy**

```bash
go generate ./internal/module/order/biz/
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/model/ internal/module/order/biz/interface.go internal/module/order/biz/restate_gen.go
git commit -m "update order model and interface, remove CancelBuyerOrder"
```

---

### Task 3: Update biz layer

**Files:**
- Modify: `internal/module/order/biz/order.go`
- Modify: `internal/module/order/biz/incoming.go:244`
- Modify: `internal/module/order/biz/pay.go:57`

- [ ] **Step 1: Delete CancelBuyerOrder from order.go**

Delete the entire `CancelBuyerOrder` method (lines 343-437) from `internal/module/order/biz/order.go`.

- [ ] **Step 2: Update ListBuyerConfirmed in order.go**

Replace the `ListBuyerConfirmed` method to use the new custom `ListCountBuyerOrder` query with `BuyerID` and `PaymentStatus`:

```go
func (b *OrderHandler) ListBuyerConfirmed(ctx restate.Context, params ListBuyerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountBuyerOrderRow, error) {
		return b.storage.Querier().ListCountBuyerOrder(ctx, orderdb.ListCountBuyerOrderParams{
			BuyerID:       params.BuyerID,
			PaymentStatus: params.PaymentStatus,
			Limit:         params.Limit,
			Offset:        params.Offset(),
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list buyer confirmed", err)
	}

	if len(listCountOrder) == 0 {
		return sharedmodel.PaginateResult[ordermodel.Order]{Data: []ordermodel.Order{}}, nil
	}

	orders := make([]orderdb.OrderOrder, len(listCountOrder))
	for i, o := range listCountOrder {
		orders[i] = o.OrderOrder
	}

	result, err := b.hydrateOrders(ctx, orders)
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[ordermodel.Order]{
		Data:  result,
		Total: listCountOrder[0].TotalCount,
	}, nil
}
```

- [ ] **Step 3: Update ListSellerConfirmed in order.go**

Update to remove `OrderStatus` param:

```go
func (b *OrderHandler) ListSellerConfirmed(ctx restate.Context, params ListSellerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	// ... same structure but remove OrderStatus from the query params:
	return b.storage.Querier().ListCountSellerOrder(ctx, orderdb.ListCountSellerOrderParams{
		SellerID:      params.SellerID,
		Search:        params.Search,
		PaymentStatus: params.PaymentStatus,
		Offset:        params.Offset(),
		Limit:         params.Limit,
	})
```

- [ ] **Step 4: Update hydrateOrders — embed Transport, remove Status**

In the `hydrateOrders` function in `order.go`, add transport fetching and update the Order construction.

After fetching payments, add transport fetching:

```go
// Fetch transports
transportIDs := lo.FilterMap(orders, func(o orderdb.OrderOrder, _ int) (uuid.UUID, bool) {
	return o.TransportID.UUID, o.TransportID.Valid
})
var transportMap map[uuid.UUID]orderdb.OrderTransport
if len(transportIDs) > 0 {
	transports, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderTransport, error) {
		return b.storage.Querier().ListTransport(ctx, orderdb.ListTransportParams{
			ID: transportIDs,
		})
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("list transports", err)
	}
	transportMap = lo.KeyBy(transports, func(t orderdb.OrderTransport) uuid.UUID { return t.ID })
}
```

Update Order construction — replace `Status` and `TransportID` with `Transport`:

```go
var transportPtr *ordermodel.Transport
if o.TransportID.Valid {
	if t, ok := transportMap[o.TransportID.UUID]; ok {
		transportPtr = &ordermodel.Transport{
			ID:          t.ID,
			Option:      t.Option,
			Status:      t.Status.OrderTransportStatus,
			Cost:        sharedmodel.Concurrency(t.Cost),
			Data:        t.Data,
			DateCreated: t.DateCreated,
		}
	}
}

result = append(result, ordermodel.Order{
	ID:              o.ID,
	BuyerID:         o.BuyerID,
	SellerID:        o.SellerID,
	Transport:       transportPtr,
	Payment:         paymentPtr,
	// Status removed
	Address:         o.Address,
	// ... rest unchanged
})
```

- [ ] **Step 5: Update incoming.go — remove Status from CreateOrder**

In `internal/module/order/biz/incoming.go`, line 244, remove `Status: orderdb.OrderStatusPending` from the `CreateOrder` call. The column no longer exists.

- [ ] **Step 6: Update pay.go — replace status check with payment_id check**

In `internal/module/order/biz/pay.go`, around line 57, replace:

```go
// OLD:
if o.Status != orderdb.OrderStatusPending {
	return fetchResult{}, ordermodel.ErrOrderNotPayable
}

// NEW:
if o.PaymentID.Valid {
	return fetchResult{}, ordermodel.ErrOrderNotPayable
}
```

- [ ] **Step 7: Build and verify**

```bash
go build ./...
```

Fix any remaining compile errors (there may be references to `o.Status` in places the agent didn't catch).

- [ ] **Step 8: Commit**

```bash
git add internal/module/order/biz/
git commit -m "update biz layer, remove CancelBuyerOrder, embed transport in order"
```

---

### Task 4: Update transport layer

**Files:**
- Modify: `internal/module/order/transport/echo/order.go`

- [ ] **Step 1: Remove CancelBuyerOrder route and handler**

In `internal/module/order/transport/echo/order.go`:

Remove the route registration line:
```go
// DELETE: buyerConfirmedApi.DELETE("/:id", h.CancelBuyerOrder)
```

Delete the `CancelBuyerOrder` handler function and its request struct (`CancelBuyerOrderRequest`).

- [ ] **Step 2: Update ListBuyerConfirmedRequest**

Replace `Status` with `PaymentStatus` and add auth:

```go
type ListBuyerConfirmedRequest struct {
	PaymentStatus []orderdb.OrderStatus `query:"payment_status"`
	sharedmodel.PaginationParams
}

func (h *Handler) ListBuyerConfirmed(c echo.Context) error {
	var req ListBuyerConfirmedRequest
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

	result, err := h.biz.ListBuyerConfirmed(c.Request().Context(), orderbiz.ListBuyerConfirmedParams{
		BuyerID:          claims.Account.ID,
		PaymentStatus:    req.PaymentStatus,
		PaginationParams: req.PaginationParams.Constrain(),
	})
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}
	return response.FromPaginate(c.Response().Writer, result)
}
```

- [ ] **Step 3: Update ListSellerConfirmedRequest — remove OrderStatus**

```go
type ListSellerConfirmedRequest struct {
	Search        null.String           `query:"search"`
	PaymentStatus []orderdb.OrderStatus `query:"payment_status"`
	sharedmodel.PaginationParams
}
```

Update the handler to remove `OrderStatus` from params mapping.

- [ ] **Step 4: Build and verify**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/module/order/transport/
git commit -m "update order transport layer, remove status filters and cancel route"
```

---

### Task 5: Frontend — update types and hooks

**Files:**
- Modify: `shopnexus-website-v3/core/order/order.buyer.ts`
- Modify: `shopnexus-website-v3/core/order/order.seller.ts`

- [ ] **Step 1: Update TOrder type — remove status, add transport**

In `core/order/order.buyer.ts`, update `TOrder`:

Remove `status: string` (line 49). Add transport object. Replace `transport_id`:

```typescript
export type TTransport = {
  id: string
  option: string
  status: string
  cost: number
  data: Record<string, any>
  date_created: string
}

export type TOrder = {
  id: string
  buyer_id: string
  seller_id: string
  transport: TTransport | null  // was transport_id
  payment: TPayment | null
  // status: string  — REMOVED
  address: string
  product_cost: number
  product_discount: number
  transport_cost: number
  total: number
  note: string | null
  data: Record<string, any>
  date_created: string
  items: TOrderItem[]
}
```

- [ ] **Step 2: Update useListBuyerConfirmed — replace status with payment_status**

```typescript
export const useListBuyerConfirmed = (params: {
  limit?: number
  payment_status?: string[]
}) =>
  useInfiniteQueryPagination<TOrder>({
    queryKey: ['order', 'buyer', 'confirmed', params],
    url: 'order/buyer/confirmed',
    params: {
      limit: params.limit ?? 20,
      ...(params.payment_status?.length ? { payment_status: params.payment_status } : {}),
    },
  })
```

- [ ] **Step 3: Update useListSellerConfirmed — remove order_status**

In `core/order/order.seller.ts`, remove `order_status` param:

```typescript
export const useListSellerConfirmed = (params: {
  limit?: number
  search?: string
  payment_status?: string[]
}) =>
  useInfiniteQueryPagination<TOrder>({
    queryKey: ['order', 'seller', 'confirmed', params],
    url: 'order/seller/confirmed',
    params: {
      limit: params.limit ?? 20,
      ...(params.search?.length ? { search: params.search } : {}),
      ...(params.payment_status?.length ? { payment_status: params.payment_status } : {}),
    },
  })
```

- [ ] **Step 4: Commit**

```bash
cd ../shopnexus-website-v3
git add core/order/
git commit -m "update order types and hooks, remove status, add transport"
```

---

### Task 6: Frontend — update buyer order pages

**Files:**
- Modify: `shopnexus-website-v3/app/(marketplace)/account/orders/page.tsx`
- Modify: `shopnexus-website-v3/app/(marketplace)/account/orders/_components/order-list.tsx`

- [ ] **Step 1: Update order tabs to use payment_status filter**

In `account/orders/page.tsx`, update the three `useListBuyerConfirmed` calls:

```typescript
// Pending tab — active orders (line 94):
// Unpaid or payment pending
} = useListBuyerConfirmed({ limit: 20, payment_status: ["Pending"] })

// Completed tab (line 225):
} = useListBuyerConfirmed({ limit: 20, payment_status: ["Success"] })

// Cancelled tab (line 233):
} = useListBuyerConfirmed({ limit: 20, payment_status: ["Cancelled", "Failed"] })
```

Also update the Pending tab to fetch unpaid orders separately (no payment = no payment_status filter, but payment_id IS NULL). This may require a new approach — or simpler: the "Pending" tab can just not pass any payment_status (shows ALL orders), and the other tabs filter them out. Alternatively, use an empty array for "no payment yet" which the backend treats as "no filter":

```typescript
// Pending tab: no payment_status filter = all non-filtered orders
// Then client-side hide orders that are Success/Cancelled/Failed
```

The simplest approach: Pending tab shows orders where payment is null or Pending. Since the backend can't easily express "NULL OR Pending" in one filter, fetch all and filter client-side in the Pending tab, or just don't filter (show all orders not yet completed).

**Recommended:** Don't pass payment_status for Pending tab. Backend returns all. FE can client-filter if needed.

- [ ] **Step 2: Update order-list.tsx — derive status from payment + transport**

Replace `statusColors` map and Badge rendering. Add a helper function:

```typescript
function getOrderDisplayStatus(order: TOrder): { label: string; color: string } {
  const ps = order.payment?.status
  const ts = order.transport?.status

  if (!order.payment) return { label: "Unpaid", color: "bg-yellow-100 text-yellow-800" }
  if (ps === "Pending") return { label: "Awaiting Payment", color: "bg-yellow-100 text-yellow-800" }
  if (ps === "Failed") return { label: "Payment Failed", color: "bg-red-100 text-red-800" }
  if (ps === "Cancelled") return { label: "Cancelled", color: "bg-red-100 text-red-800" }

  // Payment is Success — check transport
  if (ts === "Delivered") return { label: "Completed", color: "bg-green-100 text-green-800" }
  if (ts === "InTransit" || ts === "OutForDelivery") return { label: "Shipping", color: "bg-purple-100 text-purple-800" }
  if (ts === "Failed" || ts === "Cancelled") return { label: "Delivery Failed", color: "bg-red-100 text-red-800" }

  return { label: "Processing", color: "bg-blue-100 text-blue-800" }
}
```

Replace the Badge rendering (lines 143-150):
```tsx
const displayStatus = getOrderDisplayStatus(order)
// ...
<Badge variant="secondary" className={cn("font-normal", displayStatus.color)}>
  {displayStatus.label}
</Badge>
```

Update "Leave a Review" condition (line 207) — was `order.status === "Success"`:
```tsx
{order.payment?.status === "Success" && order.transport?.status === "Delivered" && order.items[0] && (
```

Remove the old `statusColors` map (lines 34-41).

- [ ] **Step 3: Commit**

```bash
git add app/(marketplace)/account/orders/
git commit -m "update buyer order pages, infer status from payment + transport"
```

---

### Task 7: Frontend — update seller order page

**Files:**
- Modify: `shopnexus-website-v3/app/(marketplace)/seller/orders/page.tsx`

- [ ] **Step 1: Update statusConfig and status rendering**

Replace `statusConfig` map (lines 72-78) with the same inference approach:

```typescript
function getOrderDisplayStatus(order: TOrder): { label: string; variant: "default" | "secondary" | "destructive" | "outline"; icon: React.ElementType } {
  const ps = order.payment?.status
  const ts = order.transport?.status

  if (!order.payment) return { label: "Unpaid", variant: "secondary", icon: Clock }
  if (ps === "Pending") return { label: "Awaiting Payment", variant: "secondary", icon: Clock }
  if (ps === "Failed") return { label: "Payment Failed", variant: "destructive", icon: XCircle }
  if (ps === "Cancelled") return { label: "Cancelled", variant: "destructive", icon: XCircle }

  if (ts === "Delivered") return { label: "Completed", variant: "outline", icon: Package }
  if (ts === "InTransit" || ts === "OutForDelivery") return { label: "Shipping", variant: "default", icon: Truck }
  if (ts === "Failed" || ts === "Cancelled") return { label: "Delivery Failed", variant: "destructive", icon: XCircle }

  return { label: "Processing", variant: "default", icon: CheckCircle }
}
```

Update line 468 from:
```typescript
const status = statusConfig[order.status] ?? statusConfig.Pending
```
to:
```typescript
const status = getOrderDisplayStatus(order)
```

Remove `order_status` from useListSellerConfirmed call if passed anywhere. Update any references to `order.status` in the confirmed tab.

Also update `order.transport_id` references to use `order.transport?.id` if any exist.

- [ ] **Step 2: Commit**

```bash
git add app/(marketplace)/seller/orders/
git commit -m "update seller order page, infer status from payment + transport"
```

---

### Task 8: Final verification

- [ ] **Step 1: Backend build**

```bash
cd /home/beanbocchi/shopnexus/shopnexus-server
go build ./...
```

- [ ] **Step 2: Run migration on local DB**

```bash
ALTER TABLE "order"."order" DROP COLUMN "status";
```

Or reset and re-migrate:
```bash
make migrate
```

- [ ] **Step 3: Verify frontend compiles**

```bash
cd ../shopnexus-website-v3
bun dev
```

Check that orders page loads, tabs work, status badges show correctly.

- [ ] **Step 4: Final commit if any fixups needed**

```bash
git add -A
git commit -m "fix remaining order status references"
```

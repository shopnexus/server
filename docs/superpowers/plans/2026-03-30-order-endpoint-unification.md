# Order Endpoint Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure all order HTTP endpoints into a `buyer/seller` hierarchy, rename biz methods + param structs to match, and add combined overview hooks in the frontend.

**Architecture:** Pure rename refactor — no DB changes, no new SQL queries, no new business logic. Transport layer gets new route structure, biz layer gets renamed methods/params, frontend hooks get renamed and re-pointed. One new biz method (`GetSellerOrder`) delegates to shared private helper.

**Tech Stack:** Go (Echo, Restate), TypeScript (Next.js, TanStack Query)

---

## File Structure

**Backend — Modify:**
- `internal/module/order/biz/interface.go` — Rename interface methods + param/result structs
- `internal/module/order/biz/checkout.go` — Rename `Checkout` → `BuyerCheckout`, `ListPendingItems` → `ListBuyerPending`, `CancelPendingItem` → `CancelBuyerPending`, update param struct refs
- `internal/module/order/biz/order.go` — Rename `GetOrder` → `GetBuyerOrder`, `ListOrders` → `ListBuyerConfirmed`, `ListSellerOrders` → `ListSellerConfirmed`, `CancelOrder` → `CancelBuyerOrder`, add `GetSellerOrder` + private `getOrder` helper, update internal calls
- `internal/module/order/biz/incoming.go` — Rename `ListIncomingItems` → `ListSellerPending`, `ConfirmItems` → `ConfirmSellerPending`, `RejectItems` → `RejectSellerPending`, update internal `b.GetOrder` → `b.GetBuyerOrder`
- `internal/module/order/biz/pay.go` — Rename `PayOrders` → `PayBuyerOrders`, update param/result struct refs
- `internal/module/order/biz/refund.go` — Rename `ListRefunds` → `ListBuyerRefunds`, `CreateRefund` → `CreateBuyerRefund`, `UpdateRefund` → `UpdateBuyerRefund`, `CancelRefund` → `CancelBuyerRefund`, `ConfirmRefund` → `ConfirmSellerRefund`, update internal `b.GetOrder` → `b.GetBuyerOrder` and `b.UpdateRefund` → `b.UpdateBuyerRefund`
- `internal/module/order/biz/restate_gen.go` — Regenerated (DO NOT EDIT manually)
- `internal/module/order/transport/echo/order.go` — Rewrite route registration, rename handler methods + request structs, update biz method calls
- `internal/module/order/transport/echo/incoming.go` — Rename handler methods + request structs, update biz method calls
- `internal/module/order/transport/echo/refund.go` — Rename handler methods + request structs, update biz method calls

**Frontend — Modify:**
- `core/order/order.buyer.ts` — Rename hooks, update API URLs
- `core/order/order.seller.ts` — Rename hooks, update API URLs
- `core/order/refund.buyer.ts` — Update API URLs from `order/refund` to `order/buyer/refund`
- `core/order/refund.seller.ts` — Update API URLs from `order/refund` and `order/refund/confirm` to `order/seller/refund` and `order/seller/refund/confirm`
- `app/(marketplace)/account/pending-items/_components/item-list.tsx` — Update hook import names
- `app/(marketplace)/account/orders/page.tsx` — Update hook import names
- `app/(marketplace)/account/orders/_components/order-list.tsx` — Update hook import names
- `app/(marketplace)/account/orders/[id]/page.tsx` — Update hook import names
- `app/(marketplace)/seller/incoming/page.tsx` — Update hook import names
- `app/(marketplace)/seller/orders/page.tsx` — Update hook import names
- `app/(marketplace)/checkout/page.tsx` — Update hook import names
- `app/(marketplace)/product/[slug]/page.tsx` — Update hook import names

**Unchanged files (no modifications needed):**
- `internal/module/order/biz/dashboard.go` — Dashboard methods keep current names
- `internal/module/order/biz/cart.go` — Cart methods keep current names
- `internal/module/order/transport/echo/cart.go` — Cart routes stay at `/order/cart`
- `core/order/cart.ts` — Cart hooks unchanged

## Master Rename Table

This table drives ALL rename tasks. Every occurrence of the "Old" name must become the "New" name.

### Biz Methods (on OrderBiz interface + OrderHandler receiver)

| Old | New | File |
|---|---|---|
| `Checkout` | `BuyerCheckout` | interface.go, checkout.go |
| `ListPendingItems` | `ListBuyerPending` | interface.go, checkout.go |
| `CancelPendingItem` | `CancelBuyerPending` | interface.go, checkout.go |
| `ListIncomingItems` | `ListSellerPending` | interface.go, incoming.go |
| `ConfirmItems` | `ConfirmSellerPending` | interface.go, incoming.go |
| `RejectItems` | `RejectSellerPending` | interface.go, incoming.go |
| `GetOrder` | `GetBuyerOrder` | interface.go, order.go |
| `ListOrders` | `ListBuyerConfirmed` | interface.go, order.go |
| `ListSellerOrders` | `ListSellerConfirmed` | interface.go, order.go |
| `CancelOrder` | `CancelBuyerOrder` | interface.go, order.go |
| `PayOrders` | `PayBuyerOrders` | interface.go, pay.go |
| `ListRefunds` | `ListBuyerRefunds` | interface.go, refund.go |
| `CreateRefund` | `CreateBuyerRefund` | interface.go, refund.go |
| `UpdateRefund` | `UpdateBuyerRefund` | interface.go, refund.go |
| `CancelRefund` | `CancelBuyerRefund` | interface.go, refund.go |
| `ConfirmRefund` | `ConfirmSellerRefund` | interface.go, refund.go |
| *(new)* | `GetSellerOrder` | interface.go, order.go |

### Param/Result Structs

| Old | New |
|---|---|
| `CheckoutParams` | `BuyerCheckoutParams` |
| `CheckoutResult` | `BuyerCheckoutResult` |
| `ListPendingItemsParams` | `ListBuyerPendingParams` |
| `CancelPendingItemParams` | `CancelBuyerPendingParams` |
| `ListIncomingItemsParams` | `ListSellerPendingParams` |
| `ConfirmItemsParams` | `ConfirmSellerPendingParams` |
| `RejectItemsParams` | `RejectSellerPendingParams` |
| `ListOrdersParams` | `ListBuyerConfirmedParams` |
| `ListSellerOrdersParams` | `ListSellerConfirmedParams` |
| `CancelOrderParams` | `CancelBuyerOrderParams` |
| `PayOrdersParams` | `PayBuyerOrdersParams` |
| `PayOrdersResult` | `PayBuyerOrdersResult` |
| `ListRefundsParams` | `ListBuyerRefundsParams` |
| `CreateRefundParams` | `CreateBuyerRefundParams` |
| `UpdateRefundParams` | `UpdateBuyerRefundParams` |
| `CancelRefundParams` | `CancelBuyerRefundParams` |
| `ConfirmRefundParams` | `ConfirmSellerRefundParams` |

### Internal Cross-References (biz methods calling other biz methods)

These `b.OldName()` calls inside biz/*.go MUST also be renamed:

| Call Site | Old Call | New Call |
|---|---|---|
| `order.go:32` (inside GetBuyerOrder) | `b.ListOrders(ctx, ListOrdersParams{...})` | `b.ListBuyerConfirmed(ctx, ListBuyerConfirmedParams{...})` |
| `order.go:341` (inside CancelBuyerOrder) | `b.GetOrder(ctx, params.OrderID)` | `b.GetBuyerOrder(ctx, params.OrderID)` |
| `incoming.go:296` (inside ConfirmSellerPending) | `b.GetOrder(ctx, orderID)` | `b.GetBuyerOrder(ctx, orderID)` |
| `refund.go:126` (inside CreateBuyerRefund) | `b.GetOrder(ctx, params.OrderID)` | `b.GetBuyerOrder(ctx, params.OrderID)` |
| `refund.go:260` (inside CancelBuyerRefund) | `b.GetOrder(ctx, refundData.OrderID)` | `b.GetBuyerOrder(ctx, refundData.OrderID)` |
| `refund.go:284` (inside ConfirmSellerRefund) | `b.UpdateRefund(ctx, UpdateRefundParams{...})` | `b.UpdateBuyerRefund(ctx, UpdateBuyerRefundParams{...})` |
| `refund.go:295` (inside ConfirmSellerRefund) | `b.GetOrder(ctx, refund.OrderID)` | `b.GetBuyerOrder(ctx, refund.OrderID)` |

### Transport Handler Methods

| Old Handler | New Handler | Old Request Struct | New Request Struct |
|---|---|---|---|
| `Checkout` | `BuyerCheckout` | `CheckoutRequest` | `BuyerCheckoutRequest` |
| `ListPendingItems` | `ListBuyerPending` | `ListPendingItemsRequest` | `ListBuyerPendingRequest` |
| `CancelPendingItem` | `CancelBuyerPending` | *(none — param from URL)* | *(same)* |
| `ListIncomingItems` | `ListSellerPending` | `ListIncomingItemsRequest` | `ListSellerPendingRequest` |
| `ConfirmItems` | `ConfirmSellerPending` | `ConfirmItemsRequest` | `ConfirmSellerPendingRequest` |
| `RejectItems` | `RejectSellerPending` | `RejectItemsRequest` | `RejectSellerPendingRequest` |
| `GetOrder` (buyer) | `GetBuyerOrder` | `GetOrderRequest` | `GetBuyerOrderRequest` |
| *(new)* | `GetSellerOrder` | *(new)* | `GetSellerOrderRequest` |
| `ListOrders` | `ListBuyerConfirmed` | `ListOrdersRequest` | `ListBuyerConfirmedRequest` |
| `ListSellerOrders` | `ListSellerConfirmed` | `ListSellerOrdersRequest` | `ListSellerConfirmedRequest` |
| `CancelOrder` | `CancelBuyerOrder` | `CancelOrderRequest` | `CancelBuyerOrderRequest` |
| `PayOrders` | `PayBuyerOrders` | `PayOrdersRequest` | `PayBuyerOrdersRequest` |
| `ListRefunds` | `ListBuyerRefunds` | `ListRefundsRequest` | `ListBuyerRefundsRequest` |
| `CreateRefund` | `CreateBuyerRefund` | `CreateRefundRequest` | `CreateBuyerRefundRequest` |
| `UpdateRefund` | `UpdateBuyerRefund` | `UpdateRefundRequest` | `UpdateBuyerRefundRequest` |
| `CancelRefund` | `CancelBuyerRefund` | `CancelRefundRequest` | `CancelBuyerRefundRequest` |
| `ConfirmRefund` | `ConfirmSellerRefund` | `ConfirmRefundRequest` | `ConfirmSellerRefundRequest` |

### Frontend Hook Renames

| File | Old Hook | New Hook | Old URL | New URL |
|---|---|---|---|---|
| order.buyer.ts | `useCheckout` | `useBuyerCheckout` | `order/checkout` | `order/buyer/checkout` |
| order.buyer.ts | `useListPendingItems` | `useListBuyerPending` | `order/checkout/items` | `order/buyer/pending` |
| order.buyer.ts | `useCancelPendingItem` | `useCancelBuyerPending` | `order/checkout/items/${id}` | `order/buyer/pending/${id}` |
| order.buyer.ts | `usePayOrders` | `usePayBuyerOrders` | `order/pay` | `order/buyer/pay` |
| order.buyer.ts | `useGetOrder` | `useGetBuyerOrder` | `order/${id}` | `order/buyer/confirmed/${id}` |
| order.buyer.ts | `useListOrders` | `useListBuyerConfirmed` | `order` | `order/buyer/confirmed` |
| order.seller.ts | `useListIncomingItems` | `useListSellerPending` | `order/incoming` | `order/seller/pending` |
| order.seller.ts | `useConfirmItems` | `useConfirmSellerPending` | `order/incoming/confirm` | `order/seller/pending/confirm` |
| order.seller.ts | `useRejectItems` | `useRejectSellerPending` | `order/incoming/reject` | `order/seller/pending/reject` |
| order.seller.ts | `useListSellerOrders` | `useListSellerConfirmed` | `order/seller` | `order/seller/confirmed` |
| refund.buyer.ts | `useCreateRefund` | *(keep name)* | `order/refund` | `order/buyer/refund` |
| refund.buyer.ts | `useListRefunds` | *(keep name)* | `order/refund` | `order/buyer/refund` |
| refund.buyer.ts | `useUpdateRefund` | *(keep name)* | `order/refund` | `order/buyer/refund` |
| refund.buyer.ts | `useCancelRefund` | *(keep name)* | `order/refund` | `order/buyer/refund` |
| refund.buyer.ts | `useConfirmRefund` | *(remove — this is a seller action)* | — | — |
| refund.seller.ts | *(all hooks)* | *(keep names)* | `order/refund` | `order/seller/refund` |
| refund.seller.ts | `useConfirmRefundSeller` | *(keep name)* | `order/refund/confirm` | `order/seller/refund/confirm` |

---

### Task 1: Rename biz interface methods and param structs

**Files:**
- Modify: `internal/module/order/biz/interface.go`

This task renames ALL methods in the `OrderBiz` interface and ALL associated param/result struct type names in `interface.go`. After this task, the file won't compile with the rest of the codebase (implementations haven't been renamed yet) — that's expected; the subsequent tasks bring everything in sync.

- [ ] **Step 1: Rename interface methods**

In `interface.go`, rename every method in the `OrderBiz` interface according to the Master Rename Table "Biz Methods" section. Also add the new `GetSellerOrder` method. The interface should become:

```go
type OrderBiz interface {
	// Buyer - Pending
	BuyerCheckout(ctx context.Context, params BuyerCheckoutParams) (BuyerCheckoutResult, error)
	ListBuyerPending(ctx context.Context, params ListBuyerPendingParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	CancelBuyerPending(ctx context.Context, params CancelBuyerPendingParams) error

	// Seller - Pending
	ListSellerPending(ctx context.Context, params ListSellerPendingParams) (sharedmodel.PaginateResult[ordermodel.OrderItem], error)
	ConfirmSellerPending(ctx context.Context, params ConfirmSellerPendingParams) (ordermodel.Order, error)
	RejectSellerPending(ctx context.Context, params RejectSellerPendingParams) error

	// Buyer - Confirmed
	GetBuyerOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	ListBuyerConfirmed(ctx context.Context, params ListBuyerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error)
	CancelBuyerOrder(ctx context.Context, params CancelBuyerOrderParams) error

	// Seller - Confirmed
	GetSellerOrder(ctx context.Context, orderID uuid.UUID) (ordermodel.Order, error)
	ListSellerConfirmed(ctx context.Context, params ListSellerConfirmedParams) (sharedmodel.PaginateResult[ordermodel.Order], error)

	// Payment
	PayBuyerOrders(ctx context.Context, params PayBuyerOrdersParams) (PayBuyerOrdersResult, error)
	ConfirmPayment(ctx context.Context, params ConfirmPaymentParams) error

	// Cart (unchanged)
	GetCart(ctx context.Context, params GetCartParams) ([]ordermodel.CartItem, error)
	UpdateCart(ctx context.Context, params UpdateCartParams) error
	ClearCart(ctx context.Context, params ClearCartParams) error

	// Review eligibility (unchanged)
	HasPurchasedProduct(ctx context.Context, params HasPurchasedProductParams) (bool, error)
	ListReviewableOrders(ctx context.Context, params ListReviewableOrdersParams) ([]ReviewableOrder, error)
	ValidateOrderForReview(ctx context.Context, params ValidateOrderForReviewParams) (bool, error)

	// Buyer - Refund
	ListBuyerRefunds(ctx context.Context, params ListBuyerRefundsParams) (sharedmodel.PaginateResult[ordermodel.Refund], error)
	CreateBuyerRefund(ctx context.Context, params CreateBuyerRefundParams) (ordermodel.Refund, error)
	UpdateBuyerRefund(ctx context.Context, params UpdateBuyerRefundParams) (ordermodel.Refund, error)
	CancelBuyerRefund(ctx context.Context, params CancelBuyerRefundParams) error

	// Seller - Refund
	ConfirmSellerRefund(ctx context.Context, params ConfirmSellerRefundParams) (ordermodel.Refund, error)

	// Dashboard (unchanged)
	GetSellerOrderStats(ctx context.Context, params GetSellerOrderStatsParams) (SellerOrderStats, error)
	GetSellerOrderTimeSeries(ctx context.Context, params GetSellerOrderTimeSeriesParams) ([]SellerOrderTimeSeriesPoint, error)
	GetSellerPendingActions(ctx context.Context, params GetSellerPendingActionsParams) (SellerPendingActions, error)
	GetSellerTopProducts(ctx context.Context, params GetSellerTopProductsParams) ([]SellerTopProduct, error)
}
```

- [ ] **Step 2: Rename all param/result struct types**

In the same file, rename every param/result struct type according to the Master Rename Table "Param/Result Structs" section. The struct bodies (fields, tags, validators) stay exactly the same — only the type names change.

Rename list:
- `CheckoutParams` → `BuyerCheckoutParams`
- `CheckoutResult` → `BuyerCheckoutResult`
- `ListPendingItemsParams` → `ListBuyerPendingParams`
- `CancelPendingItemParams` → `CancelBuyerPendingParams`
- `ListIncomingItemsParams` → `ListSellerPendingParams`
- `ConfirmItemsParams` → `ConfirmSellerPendingParams`
- `RejectItemsParams` → `RejectSellerPendingParams`
- `ListOrdersParams` → `ListBuyerConfirmedParams`
- `ListSellerOrdersParams` → `ListSellerConfirmedParams`
- `CancelOrderParams` → `CancelBuyerOrderParams`
- `PayOrdersParams` → `PayBuyerOrdersParams`
- `PayOrdersResult` → `PayBuyerOrdersResult`
- `ListRefundsParams` → `ListBuyerRefundsParams`
- `CreateRefundParams` → `CreateBuyerRefundParams`
- `UpdateRefundParams` → `UpdateBuyerRefundParams`
- `CancelRefundParams` → `CancelBuyerRefundParams`
- `ConfirmRefundParams` → `ConfirmSellerRefundParams`

Do NOT rename: `CheckoutItem`, `CheckoutItemRequest`, `GetCartParams`, `UpdateCartParams`, `ClearCartParams`, `ConfirmPaymentParams`, `HasPurchasedProductParams`, `ListReviewableOrdersParams`, `ReviewableOrder`, `ValidateOrderForReviewParams`, or any dashboard structs.

- [ ] **Step 3: Commit**

```bash
git add internal/module/order/biz/interface.go
git commit -m "rename order biz interface methods and param structs"
```

---

### Task 2: Rename biz implementations in checkout.go

**Files:**
- Modify: `internal/module/order/biz/checkout.go`

Rename method receivers and all references to renamed param/result structs.

- [ ] **Step 1: Rename methods and struct references**

Apply these renames throughout `checkout.go`:

| Find | Replace |
|---|---|
| `func (b *OrderHandler) Checkout(` | `func (b *OrderHandler) BuyerCheckout(` |
| `func (b *OrderHandler) ListPendingItems(` | `func (b *OrderHandler) ListBuyerPending(` |
| `func (b *OrderHandler) CancelPendingItem(` | `func (b *OrderHandler) CancelBuyerPending(` |
| `CheckoutParams` | `BuyerCheckoutParams` |
| `CheckoutResult` | `BuyerCheckoutResult` |
| `ListPendingItemsParams` | `ListBuyerPendingParams` |
| `CancelPendingItemParams` | `CancelBuyerPendingParams` |

Also rename the doc comments:
- `// Checkout creates` → `// BuyerCheckout creates`
- `// ListPendingItems returns` → `// ListBuyerPending returns`
- `// CancelPendingItem cancels` → `// CancelBuyerPending cancels`

Do NOT rename: `CheckoutItem` (this is a sub-struct, not a param type), `hydrateItems`, `enrichItems` (these are private helpers).

- [ ] **Step 2: Commit**

```bash
git add internal/module/order/biz/checkout.go
git commit -m "rename checkout biz methods to buyer prefix"
```

---

### Task 3: Rename biz implementations in order.go + add GetSellerOrder

**Files:**
- Modify: `internal/module/order/biz/order.go`

This is the most complex biz file. It has internal cross-references: `GetOrder` calls `ListOrders`, and `CancelOrder` calls `GetOrder`.

- [ ] **Step 1: Rename methods, struct references, and internal calls**

Apply these renames throughout `order.go`:

| Find | Replace |
|---|---|
| `func (b *OrderHandler) GetOrder(` | `func (b *OrderHandler) GetBuyerOrder(` |
| `func (b *OrderHandler) ListOrders(` | `func (b *OrderHandler) ListBuyerConfirmed(` |
| `func (b *OrderHandler) ListSellerOrders(` | `func (b *OrderHandler) ListSellerConfirmed(` |
| `func (b *OrderHandler) CancelOrder(` | `func (b *OrderHandler) CancelBuyerOrder(` |
| `ListOrdersParams` | `ListBuyerConfirmedParams` |
| `ListSellerOrdersParams` | `ListSellerConfirmedParams` |
| `CancelOrderParams` | `CancelBuyerOrderParams` |

**Critical internal call updates:**
- In `GetBuyerOrder` (was `GetOrder`), line ~32: change `b.ListOrders(ctx, ListOrdersParams{` → `b.ListBuyerConfirmed(ctx, ListBuyerConfirmedParams{`
- In `CancelBuyerOrder` (was `CancelOrder`), line ~341: change `b.GetOrder(ctx, params.OrderID)` → `b.GetBuyerOrder(ctx, params.OrderID)`

Update doc comments to match new method names.

Do NOT rename: `ConfirmPayment`, `HasPurchasedProduct`, `ListReviewableOrders`, `ValidateOrderForReview`, `hydrateOrders` (these stay as-is).

- [ ] **Step 2: Add GetSellerOrder method**

Add this method to `order.go`, right after `GetBuyerOrder`:

```go
// GetSellerOrder returns a single order by ID (seller perspective).
// Uses the same underlying logic as GetBuyerOrder.
func (b *OrderHandler) GetSellerOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	return b.GetBuyerOrder(ctx, orderID)
}
```

Note: Both methods share the same logic. The transport layer handles the auth differentiation (buyer checks `order.buyer_id`, seller checks `order.seller_id`).

- [ ] **Step 3: Commit**

```bash
git add internal/module/order/biz/order.go
git commit -m "rename order biz methods, add GetSellerOrder"
```

---

### Task 4: Rename biz implementations in incoming.go

**Files:**
- Modify: `internal/module/order/biz/incoming.go`

- [ ] **Step 1: Rename methods, struct references, and internal calls**

Apply these renames throughout `incoming.go`:

| Find | Replace |
|---|---|
| `func (b *OrderHandler) ListIncomingItems(` | `func (b *OrderHandler) ListSellerPending(` |
| `func (b *OrderHandler) ConfirmItems(` | `func (b *OrderHandler) ConfirmSellerPending(` |
| `func (b *OrderHandler) RejectItems(` | `func (b *OrderHandler) RejectSellerPending(` |
| `ListIncomingItemsParams` | `ListSellerPendingParams` |
| `ConfirmItemsParams` | `ConfirmSellerPendingParams` |
| `RejectItemsParams` | `RejectSellerPendingParams` |

**Critical internal call update:**
- In `ConfirmSellerPending` (was `ConfirmItems`), line ~296: change `b.GetOrder(ctx, orderID)` → `b.GetBuyerOrder(ctx, orderID)`

Update doc comments to match new method names.

- [ ] **Step 2: Commit**

```bash
git add internal/module/order/biz/incoming.go
git commit -m "rename incoming biz methods to seller pending prefix"
```

---

### Task 5: Rename biz implementations in pay.go and refund.go

**Files:**
- Modify: `internal/module/order/biz/pay.go`
- Modify: `internal/module/order/biz/refund.go`

- [ ] **Step 1: Rename pay.go**

Apply these renames throughout `pay.go`:

| Find | Replace |
|---|---|
| `func (b *OrderHandler) PayOrders(` | `func (b *OrderHandler) PayBuyerOrders(` |
| `PayOrdersParams` | `PayBuyerOrdersParams` |
| `PayOrdersResult` | `PayBuyerOrdersResult` |

Update doc comments. Do NOT rename `payWithRedirect`, `payWithSavedMethod`, `fetchPaymentResult` (private helpers).

- [ ] **Step 2: Rename refund.go**

Apply these renames throughout `refund.go`:

| Find | Replace |
|---|---|
| `func (b *OrderHandler) ListRefunds(` | `func (b *OrderHandler) ListBuyerRefunds(` |
| `func (b *OrderHandler) CreateRefund(` | `func (b *OrderHandler) CreateBuyerRefund(` |
| `func (b *OrderHandler) UpdateRefund(` | `func (b *OrderHandler) UpdateBuyerRefund(` |
| `func (b *OrderHandler) CancelRefund(` | `func (b *OrderHandler) CancelBuyerRefund(` |
| `func (b *OrderHandler) ConfirmRefund(` | `func (b *OrderHandler) ConfirmSellerRefund(` |
| `ListRefundsParams` | `ListBuyerRefundsParams` |
| `CreateRefundParams` | `CreateBuyerRefundParams` |
| `UpdateRefundParams` | `UpdateBuyerRefundParams` |
| `CancelRefundParams` | `CancelBuyerRefundParams` |
| `ConfirmRefundParams` | `ConfirmSellerRefundParams` |

**Critical internal call updates in refund.go:**
- In `CreateBuyerRefund`, line ~126: `b.GetOrder(ctx,` → `b.GetBuyerOrder(ctx,`
- In `CancelBuyerRefund`, line ~260: `b.GetOrder(ctx,` → `b.GetBuyerOrder(ctx,`
- In `ConfirmSellerRefund`, line ~284: `b.UpdateRefund(ctx, UpdateRefundParams{` → `b.UpdateBuyerRefund(ctx, UpdateBuyerRefundParams{`
- In `ConfirmSellerRefund`, line ~295: `b.GetOrder(ctx,` → `b.GetBuyerOrder(ctx,`

Update doc comments. Do NOT rename: `dbToRefund` (private helper).

- [ ] **Step 3: Commit**

```bash
git add internal/module/order/biz/pay.go internal/module/order/biz/refund.go
git commit -m "rename pay and refund biz methods"
```

---

### Task 6: Rewrite transport layer route registration and handler methods

**Files:**
- Modify: `internal/module/order/transport/echo/order.go`
- Modify: `internal/module/order/transport/echo/incoming.go`
- Modify: `internal/module/order/transport/echo/refund.go`

- [ ] **Step 1: Rewrite route registration in order.go**

Replace the route registration block in `NewHandler` with the new URL structure:

```go
func NewHandler(e *echo.Echo, biz orderbiz.OrderBiz, handler *orderbiz.OrderHandler) *Handler {
	h := &Handler{biz: biz}
	g := e.Group("/api/v1/order")

	// Cart (unchanged)
	g.GET("/cart", h.GetCart)
	g.POST("/cart", h.UpdateCart)
	g.DELETE("/cart", h.ClearCart)

	// Buyer - Pending
	g.POST("/buyer/checkout", h.BuyerCheckout)
	g.GET("/buyer/pending", h.ListBuyerPending)
	g.DELETE("/buyer/pending/:id", h.CancelBuyerPending)

	// Buyer - Confirmed
	g.GET("/buyer/confirmed", h.ListBuyerConfirmed)
	g.GET("/buyer/confirmed/:id", h.GetBuyerOrder)
	g.DELETE("/buyer/confirmed/:id", h.CancelBuyerOrder)
	g.POST("/buyer/pay", h.PayBuyerOrders)

	// Buyer - Refund
	buyerRefund := g.Group("/buyer/refund")
	buyerRefund.GET("", h.ListBuyerRefunds)
	buyerRefund.POST("", h.CreateBuyerRefund)
	buyerRefund.PATCH("", h.UpdateBuyerRefund)
	buyerRefund.DELETE("", h.CancelBuyerRefund)

	// Seller - Pending
	g.GET("/seller/pending", h.ListSellerPending)
	g.POST("/seller/pending/confirm", h.ConfirmSellerPending)
	g.POST("/seller/pending/reject", h.RejectSellerPending)

	// Seller - Confirmed
	g.GET("/seller/confirmed", h.ListSellerConfirmed)
	g.GET("/seller/confirmed/:id", h.GetSellerOrder)

	// Seller - Refund
	g.POST("/seller/refund/confirm", h.ConfirmSellerRefund)

	// Payment webhooks (unchanged)
	onResult := func(ctx context.Context, result payment.WebhookResult) error {
		return biz.ConfirmPayment(ctx, orderbiz.ConfirmPaymentParams{
			RefID:  result.RefID,
			Status: result.Status,
		})
	}
	for _, client := range handler.PaymentClients() {
		client.OnResult(onResult)
		client.InitializeWebhook(e)
	}

	return h
}
```

- [ ] **Step 2: Rename handler methods and request structs in order.go**

Rename all handler methods and their associated request structs in `order.go`:

| Find | Replace |
|---|---|
| `GetOrderRequest` | `GetBuyerOrderRequest` |
| `func (h *Handler) GetOrder(` | `func (h *Handler) GetBuyerOrder(` |
| `h.biz.GetOrder(` | `h.biz.GetBuyerOrder(` |
| `ListOrdersRequest` | `ListBuyerConfirmedRequest` |
| `func (h *Handler) ListOrders(` | `func (h *Handler) ListBuyerConfirmed(` |
| `h.biz.ListOrders(` | `h.biz.ListBuyerConfirmed(` |
| `orderbiz.ListOrdersParams` | `orderbiz.ListBuyerConfirmedParams` |
| `ListSellerOrdersRequest` | `ListSellerConfirmedRequest` |
| `func (h *Handler) ListSellerOrders(` | `func (h *Handler) ListSellerConfirmed(` |
| `h.biz.ListSellerOrders(` | `h.biz.ListSellerConfirmed(` |
| `orderbiz.ListSellerOrdersParams` | `orderbiz.ListSellerConfirmedParams` |
| `CheckoutRequest` | `BuyerCheckoutRequest` |
| `CheckoutItemRequest` | `BuyerCheckoutItemRequest` |
| `func (h *Handler) Checkout(` | `func (h *Handler) BuyerCheckout(` |
| `h.biz.Checkout(` | `h.biz.BuyerCheckout(` |
| `orderbiz.CheckoutParams` | `orderbiz.BuyerCheckoutParams` |
| `orderbiz.CheckoutItem` | `orderbiz.CheckoutItem` *(unchanged — sub-struct)* |
| `ListPendingItemsRequest` | `ListBuyerPendingRequest` |
| `func (h *Handler) ListPendingItems(` | `func (h *Handler) ListBuyerPending(` |
| `h.biz.ListPendingItems(` | `h.biz.ListBuyerPending(` |
| `orderbiz.ListPendingItemsParams` | `orderbiz.ListBuyerPendingParams` |
| `func (h *Handler) CancelPendingItem(` | `func (h *Handler) CancelBuyerPending(` |
| `h.biz.CancelPendingItem(` | `h.biz.CancelBuyerPending(` |
| `orderbiz.CancelPendingItemParams` | `orderbiz.CancelBuyerPendingParams` |
| `PayOrdersRequest` | `PayBuyerOrdersRequest` |
| `func (h *Handler) PayOrders(` | `func (h *Handler) PayBuyerOrders(` |
| `h.biz.PayOrders(` | `h.biz.PayBuyerOrders(` |
| `orderbiz.PayOrdersParams` | `orderbiz.PayBuyerOrdersParams` |
| `CancelOrderRequest` | `CancelBuyerOrderRequest` |
| `func (h *Handler) CancelOrder(` | `func (h *Handler) CancelBuyerOrder(` |
| `h.biz.CancelOrder(` | `h.biz.CancelBuyerOrder(` |
| `orderbiz.CancelOrderParams` | `orderbiz.CancelBuyerOrderParams` |

Add new `GetSellerOrder` handler method (after `GetBuyerOrder`):

```go
type GetSellerOrderRequest struct {
	ID uuid.UUID `param:"id" validate:"required"`
}

func (h *Handler) GetSellerOrder(c echo.Context) error {
	var req GetSellerOrderRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}
	if err := c.Validate(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	if _, err := authclaims.GetClaims(c.Request()); err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	result, err := h.biz.GetSellerOrder(c.Request().Context(), req.ID)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}
```

- [ ] **Step 3: Rename handler methods and request structs in incoming.go**

| Find | Replace |
|---|---|
| `ListIncomingItemsRequest` | `ListSellerPendingRequest` |
| `func (h *Handler) ListIncomingItems(` | `func (h *Handler) ListSellerPending(` |
| `h.biz.ListIncomingItems(` | `h.biz.ListSellerPending(` |
| `orderbiz.ListIncomingItemsParams` | `orderbiz.ListSellerPendingParams` |
| `ConfirmItemsRequest` | `ConfirmSellerPendingRequest` |
| `func (h *Handler) ConfirmItems(` | `func (h *Handler) ConfirmSellerPending(` |
| `h.biz.ConfirmItems(` | `h.biz.ConfirmSellerPending(` |
| `orderbiz.ConfirmItemsParams` | `orderbiz.ConfirmSellerPendingParams` |
| `RejectItemsRequest` | `RejectSellerPendingRequest` |
| `func (h *Handler) RejectItems(` | `func (h *Handler) RejectSellerPending(` |
| `h.biz.RejectItems(` | `h.biz.RejectSellerPending(` |
| `orderbiz.RejectItemsParams` | `orderbiz.RejectSellerPendingParams` |

- [ ] **Step 4: Rename handler methods and request structs in refund.go**

| Find | Replace |
|---|---|
| `ListRefundsRequest` | `ListBuyerRefundsRequest` |
| `func (h *Handler) ListRefunds(` | `func (h *Handler) ListBuyerRefunds(` |
| `h.biz.ListRefunds(` | `h.biz.ListBuyerRefunds(` |
| `orderbiz.ListRefundsParams` | `orderbiz.ListBuyerRefundsParams` |
| `CreateRefundRequest` | `CreateBuyerRefundRequest` |
| `func (h *Handler) CreateRefund(` | `func (h *Handler) CreateBuyerRefund(` |
| `h.biz.CreateRefund(` | `h.biz.CreateBuyerRefund(` |
| `orderbiz.CreateRefundParams` | `orderbiz.CreateBuyerRefundParams` |
| `UpdateRefundRequest` | `UpdateBuyerRefundRequest` |
| `func (h *Handler) UpdateRefund(` | `func (h *Handler) UpdateBuyerRefund(` |
| `h.biz.UpdateRefund(` | `h.biz.UpdateBuyerRefund(` |
| `orderbiz.UpdateRefundParams` | `orderbiz.UpdateBuyerRefundParams` |
| `CancelRefundRequest` | `CancelBuyerRefundRequest` |
| `func (h *Handler) CancelRefund(` | `func (h *Handler) CancelBuyerRefund(` |
| `h.biz.CancelRefund(` | `h.biz.CancelBuyerRefund(` |
| `orderbiz.CancelRefundParams` | `orderbiz.CancelBuyerRefundParams` |
| `ConfirmRefundRequest` | `ConfirmSellerRefundRequest` |
| `func (h *Handler) ConfirmRefund(` | `func (h *Handler) ConfirmSellerRefund(` |
| `h.biz.ConfirmRefund(` | `h.biz.ConfirmSellerRefund(` |
| `orderbiz.ConfirmRefundParams` | `orderbiz.ConfirmSellerRefundParams` |

- [ ] **Step 5: Commit**

```bash
git add internal/module/order/transport/echo/order.go internal/module/order/transport/echo/incoming.go internal/module/order/transport/echo/refund.go
git commit -m "rewrite order transport layer with buyer/seller URL hierarchy"
```

---

### Task 7: Regenerate restate_gen.go and verify compilation

**Files:**
- Regenerate: `internal/module/order/biz/restate_gen.go`

- [ ] **Step 1: Regenerate Restate proxy**

```bash
go generate ./internal/module/order/biz/
```

This reads the updated `OrderBiz` interface from `interface.go` and regenerates `restate_gen.go` with matching method names.

- [ ] **Step 2: Verify backend compiles**

```bash
go build ./...
```

Expected: Clean compilation with no errors.

If there are compile errors, they will be from missed renames. Check the error messages — they will point to the exact file and line where an old name is still used. Fix by applying the appropriate rename from the Master Rename Table.

- [ ] **Step 3: Commit**

```bash
git add internal/module/order/biz/restate_gen.go
git commit -m "regenerate order restate proxy"
```

---

### Task 8: Update frontend hooks — order.buyer.ts

**Files:**
- Modify: `../shopnexus-website-v3/core/order/order.buyer.ts`

- [ ] **Step 1: Rename hooks and update API URLs**

Apply these changes to `order.buyer.ts`:

1. `useCheckout` → `useBuyerCheckout`, URL: `order/checkout` → `order/buyer/checkout`
2. `useListPendingItems` → `useListBuyerPending`, URL: `order/checkout/items` → `order/buyer/pending`, query key: `['order', 'buyer', 'pending']`
3. `useCancelPendingItem` → `useCancelBuyerPending`, URL: `` `order/checkout/items/${id}` `` → `` `order/buyer/pending/${id}` ``, query key: `['order', 'buyer', 'pending', 'cancel']`, invalidate key: `['order', 'buyer', 'pending']`
4. `usePayOrders` → `usePayBuyerOrders`, URL: `order/pay` → `order/buyer/pay`
5. `useGetOrder` → `useGetBuyerOrder`, URL: `` `order/${id}` `` → `` `order/buyer/confirmed/${id}` ``
6. `useListOrders` → `useListBuyerConfirmed`, URL: `order` → `order/buyer/confirmed`, query key: `['order', 'buyer', 'confirmed']`

Update `onSuccess` invalidation keys to match the new query keys:
- `useBuyerCheckout` should invalidate `['order', 'buyer', 'pending']` (was `['order', 'checkout', 'items']`)
- `usePayBuyerOrders` should invalidate `['order', 'buyer', 'confirmed']` (was `['order']`)

Keep all type exports (`TOrderItem`, `TOrder`, `TPayment`, `OrderItemStatus`) unchanged — they are imported by other files.

- [ ] **Step 2: Commit**

```bash
cd ../shopnexus-website-v3 && git add core/order/order.buyer.ts
git commit -m "rename buyer order hooks with new URL structure"
```

---

### Task 9: Update frontend hooks — order.seller.ts

**Files:**
- Modify: `../shopnexus-website-v3/core/order/order.seller.ts`

- [ ] **Step 1: Rename hooks and update API URLs**

Apply these changes to `order.seller.ts`:

1. `useListIncomingItems` → `useListSellerPending`, URL: `order/incoming` → `order/seller/pending`, query key: `['order', 'seller', 'pending']`
2. `useConfirmItems` → `useConfirmSellerPending`, URL: `order/incoming/confirm` → `order/seller/pending/confirm`, mutation key: `['order', 'seller', 'pending', 'confirm']`, invalidate keys: `['order', 'seller', 'pending']` and `['order', 'seller', 'confirmed']`
3. `useRejectItems` → `useRejectSellerPending`, URL: `order/incoming/reject` → `order/seller/pending/reject`, mutation key: `['order', 'seller', 'pending', 'reject']`, invalidate key: `['order', 'seller', 'pending']`
4. `useListSellerOrders` → `useListSellerConfirmed`, URL: `order/seller` → `order/seller/confirmed`, query key: `['order', 'seller', 'confirmed']`

Add new `useGetSellerOrder` hook:

```typescript
export const useGetSellerOrder = (id: string) =>
  useQuery({
    queryKey: ['order', 'seller', 'confirmed', id],
    queryFn: () => customFetchStandard<TOrder>(`order/seller/confirmed/${id}`),
    enabled: !!id,
  })
```

This requires adding `useQuery` to the imports from `@tanstack/react-query` and importing `TOrder` from `./order.buyer`.

- [ ] **Step 2: Commit**

```bash
cd ../shopnexus-website-v3 && git add core/order/order.seller.ts
git commit -m "rename seller order hooks with new URL structure"
```

---

### Task 10: Update frontend hooks — refund files

**Files:**
- Modify: `../shopnexus-website-v3/core/order/refund.buyer.ts`
- Modify: `../shopnexus-website-v3/core/order/refund.seller.ts`

- [ ] **Step 1: Update refund.buyer.ts URLs**

Change all API URLs from `order/refund` to `order/buyer/refund`:

| Hook | Old URL | New URL |
|---|---|---|
| `useCreateRefund` | `order/refund` | `order/buyer/refund` |
| `useListRefunds` | `order/refund` | `order/buyer/refund` |
| `useUpdateRefund` | `order/refund` | `order/buyer/refund` |
| `useCancelRefund` | `order/refund` | `order/buyer/refund` |

Remove `useConfirmRefund` from this file — it's a seller action and already exists in `refund.seller.ts`.

- [ ] **Step 2: Update refund.seller.ts URLs**

Change API URLs:

| Hook | Old URL | New URL |
|---|---|---|
| `useListRefundsSeller` | `order/refund` | `order/seller/refund` |
| `useUpdateRefundSeller` | `order/refund` | `order/seller/refund` |
| `useCancelRefundSeller` | `order/refund` | `order/seller/refund` |
| `useConfirmRefundSeller` | `order/refund/confirm` | `order/seller/refund/confirm` |

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3 && git add core/order/refund.buyer.ts core/order/refund.seller.ts
git commit -m "update refund hook URLs to buyer/seller paths"
```

---

### Task 11: Update all frontend consuming pages

**Files:**
- Modify: `../shopnexus-website-v3/app/(marketplace)/account/pending-items/_components/item-list.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/account/orders/page.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/account/orders/_components/order-list.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/account/orders/[id]/page.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/seller/incoming/page.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/seller/orders/page.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/checkout/page.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/product/[slug]/page.tsx`

- [ ] **Step 1: Update imports in each file**

For each file, update the import names to match the renamed hooks. Read each file first to see which hooks it imports, then apply the rename.

**Import rename map:**

| Old Import | New Import |
|---|---|
| `useCheckout` | `useBuyerCheckout` |
| `useListPendingItems` | `useListBuyerPending` |
| `useCancelPendingItem` | `useCancelBuyerPending` |
| `usePayOrders` | `usePayBuyerOrders` |
| `useGetOrder` | `useGetBuyerOrder` |
| `useListOrders` | `useListBuyerConfirmed` |
| `useListIncomingItems` | `useListSellerPending` |
| `useConfirmItems` | `useConfirmSellerPending` |
| `useRejectItems` | `useRejectSellerPending` |
| `useListSellerOrders` | `useListSellerConfirmed` |
| `useConfirmRefund` | *(remove — now in refund.seller.ts)* |

Also update all USAGES of these hooks in the component bodies (the hook call sites, not just imports).

Type exports (`TOrderItem`, `TOrder`, `TPayment`, `OrderItemStatus`) are NOT renamed, so those imports stay the same.

- [ ] **Step 2: Verify frontend compiles**

```bash
cd ../shopnexus-website-v3 && npx tsc --noEmit
```

Expected: No type errors.

If there are errors, they will point to old hook names still in use. Fix by applying the rename map above.

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3 && git add .
git commit -m "update all pages to use renamed order hooks"
```

---

### Task 12: Add overview hooks and update overview pages

**Files:**
- Modify: `../shopnexus-website-v3/core/order/order.buyer.ts`
- Modify: `../shopnexus-website-v3/core/order/order.seller.ts`
- Modify: `../shopnexus-website-v3/app/(marketplace)/account/orders/page.tsx`
- Modify: `../shopnexus-website-v3/app/(marketplace)/seller/orders/page.tsx` (or `../shopnexus-website-v3/app/(marketplace)/seller/page.tsx` — the seller landing)

- [ ] **Step 1: Add useGetBuyerOverview hook**

Add to `order.buyer.ts`:

```typescript
export const useGetBuyerOverview = () => {
  const pending = useListBuyerPending({ limit: 20 })
  const confirmed = useListBuyerConfirmed({ limit: 20 })
  return {
    pendingItems: pending.data?.pages.flatMap(p => p.data) ?? [],
    orders: confirmed.data?.pages.flatMap(p => p.data) ?? [],
    isLoading: pending.isLoading || confirmed.isLoading,
  }
}
```

- [ ] **Step 2: Add useGetSellerOverview hook**

Add to `order.seller.ts`:

```typescript
export const useGetSellerOverview = (params?: { search?: string }) => {
  const pending = useListSellerPending({ limit: 20, ...params })
  const confirmed = useListSellerConfirmed({ limit: 20, ...params })
  return {
    incomingItems: pending.data?.pages.flatMap(p => p.data) ?? [],
    orders: confirmed.data?.pages.flatMap(p => p.data) ?? [],
    isLoading: pending.isLoading || confirmed.isLoading,
  }
}
```

- [ ] **Step 3: Update buyer orders page**

In `../shopnexus-website-v3/app/(marketplace)/account/orders/page.tsx`, add a "Pending Items" section above the existing orders list. Use `useGetBuyerOverview()` or add `useListBuyerPending` directly. Show pending items in a compact card list with a "View all" link to `/account/pending-items`. Read the current page to understand its structure before modifying.

- [ ] **Step 4: Update seller orders page**

In the seller orders/landing page, add an "Incoming Items" section above the existing orders list. Use `useGetSellerOverview()` or add `useListSellerPending` directly. Show incoming items in a compact card list with a "View all" link to `/seller/incoming`. Read the current page to understand its structure before modifying.

- [ ] **Step 5: Verify frontend compiles**

```bash
cd ../shopnexus-website-v3 && npx tsc --noEmit
```

- [ ] **Step 6: Commit**

```bash
cd ../shopnexus-website-v3 && git add .
git commit -m "add buyer/seller overview hooks and update overview pages"
```

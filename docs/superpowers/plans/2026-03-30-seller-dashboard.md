# Seller Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a seller dashboard API endpoint to the analytic module and wire it into the existing frontend seller dashboard page with real data, charts, and date range filtering.

**Architecture:** Extend the analytic module with a `GetSellerDashboard` biz method that aggregates data from order, catalog, and inventory modules via Restate proxies. Add new SQL queries to the order module for seller stats aggregation. On the frontend, install Recharts, create a dashboard API hook, and rewrite the seller dashboard page to use real data.

**Tech Stack:** Go (Echo + Restate + SQLC + PostgreSQL), TypeScript (Next.js 16 + React 19 + TanStack Query + Recharts + shadcn/ui)

---

## File Map

### Backend — New Files
- `internal/module/analytic/model/dashboard.go` — Dashboard request/response DTOs
- `internal/module/analytic/biz/dashboard.go` — `GetSellerDashboard` biz method
- `internal/module/order/db/queries/dashboard_custom.sql` — SQL queries for seller dashboard aggregation

### Backend — Modified Files
- `internal/module/analytic/model/analytic.go` — Add 2 new event constants
- `internal/module/analytic/config/config.go` — Add popularity weights for new events
- `internal/module/analytic/biz/interface.go` — Add `GetSellerDashboard` to `AnalyticBiz` interface + new deps
- `internal/module/analytic/fx.go` — Wire `OrderBiz` + `CatalogBiz` deps into `AnalyticHandler`
- `internal/module/analytic/transport/echo/analytic.go` — Add `/seller-dashboard` route + handler
- `internal/module/order/biz/interface.go` — Add 4 new methods to `OrderBiz` interface
- `internal/module/order/biz/dashboard.go` — Implement the 4 new order dashboard methods

### Backend — Auto-Generated (after running codegen)
- `internal/module/analytic/biz/restate_gen.go` — Regenerated
- `internal/module/order/biz/restate_gen.go` — Regenerated
- `internal/module/order/db/sqlc/*` — Regenerated from new SQL queries

### Frontend — New Files
- `shopnexus-website-v3/core/analytic/dashboard.ts` — Dashboard API hook + types

### Frontend — Modified Files
- `shopnexus-website-v3/app/(marketplace)/seller/page.tsx` — Full rewrite with real data

### Frontend — Dependencies to Install
- `recharts` — charting library
- `date-fns` — date manipulation for range presets

---

### Task 1: Add New Analytics Event Constants

**Files:**
- Modify: `internal/module/analytic/model/analytic.go:11-43`
- Modify: `internal/module/analytic/config/config.go`

- [ ] **Step 1: Add event constants**

In `internal/module/analytic/model/analytic.go`, add two new constants in the appropriate sections:

```go
// After the "Discovery events" block (after EventViewSimilarProducts), add:

	// Funnel events
	EventProductImpression = "product_impression"
	EventCheckoutStarted   = "checkout_started"
```

- [ ] **Step 2: Add popularity weights for new events**

In `internal/module/analytic/config/config.go`, add fields to `PopularityWeights` struct:

```go
// Add these fields after ViewSimilarProducts:
	ProductImpression   float64 `yaml:"product_impression" mapstructure:"product_impression"`
	CheckoutStarted     float64 `yaml:"checkout_started" mapstructure:"checkout_started"`
```

In `DefaultPopularityWeights()`, add defaults:

```go
		ProductImpression:   0.1,
		CheckoutStarted:     0.6,
```

In `WeightMap()`, add entries:

```go
		"product_impression":    w.ProductImpression,
		"checkout_started":      w.CheckoutStarted,
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/module/analytic/...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/module/analytic/model/analytic.go internal/module/analytic/config/config.go
git commit -m "add product_impression and checkout_started analytics events"
```

---

### Task 2: Add Dashboard SQL Queries to Order Module

**Files:**
- Create: `internal/module/order/db/queries/dashboard_custom.sql`

- [ ] **Step 1: Write the dashboard SQL queries**

Create `internal/module/order/db/queries/dashboard_custom.sql`:

```sql
-- Custom dashboard aggregation queries for seller analytics

-- name: GetSellerOrderStats :one
-- Aggregates revenue, order count, and items sold for a seller within a date range.
-- Only counts orders with status 'Success'.
SELECT
    COALESCE(SUM(o."total"), 0)::bigint AS total_revenue,
    COUNT(o."id")::bigint AS total_orders,
    COALESCE(SUM(i.item_count), 0)::bigint AS items_sold
FROM "order"."order" o
LEFT JOIN LATERAL (
    SELECT SUM(it."quantity") AS item_count
    FROM "order"."item" it
    WHERE it."order_id" = o."id" AND it."status" = 'Confirmed'
) i ON true
WHERE o."seller_id" = @seller_id
    AND o."status" = 'Success'
    AND o."date_created" >= @start_date
    AND o."date_created" < @end_date;

-- name: GetSellerOrderTimeSeries :many
-- Returns time-bucketed revenue and order counts for chart data.
-- @granularity must be 'day', 'week', or 'month'.
SELECT
    date_trunc(@granularity::text, o."date_created")::timestamptz AS bucket,
    COALESCE(SUM(o."total"), 0)::bigint AS revenue,
    COUNT(o."id")::bigint AS order_count
FROM "order"."order" o
WHERE o."seller_id" = @seller_id
    AND o."status" = 'Success'
    AND o."date_created" >= @start_date
    AND o."date_created" < @end_date
GROUP BY bucket
ORDER BY bucket ASC;

-- name: GetSellerPendingActions :one
-- Counts unconfirmed incoming items and pending refunds for a seller.
SELECT
    (SELECT COUNT(*)::bigint FROM "order"."item"
     WHERE "seller_id" = @seller_id AND "status" = 'Pending' AND "order_id" IS NULL) AS pending_items,
    (SELECT COUNT(*)::bigint FROM "order"."refund" r
     JOIN "order"."order" o ON r."order_id" = o."id"
     WHERE o."seller_id" = @seller_id AND r."status" = 'Pending') AS pending_refunds;

-- name: GetSellerTopProducts :many
-- Returns top products by sold quantity within a date range, with revenue.
SELECT
    i."sku_id",
    i."sku_name",
    SUM(i."quantity")::bigint AS sold_count,
    SUM(i."paid_amount")::bigint AS revenue
FROM "order"."item" i
JOIN "order"."order" o ON i."order_id" = o."id"
WHERE o."seller_id" = @seller_id
    AND o."status" = 'Success'
    AND o."date_created" >= @start_date
    AND o."date_created" < @end_date
GROUP BY i."sku_id", i."sku_name"
ORDER BY sold_count DESC
LIMIT @top_limit::int;
```

- [ ] **Step 2: Run SQLC code generation**

Run: `sqlc generate`
Expected: generates new Go code in `internal/module/order/db/sqlc/` with the 4 new query functions

- [ ] **Step 3: Verify generated code compiles**

Run: `go build ./internal/module/order/db/sqlc/...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/module/order/db/queries/dashboard_custom.sql internal/module/order/db/sqlc/
git commit -m "add seller dashboard SQL queries to order module"
```

---

### Task 3: Add Dashboard Methods to OrderBiz Interface

**Files:**
- Modify: `internal/module/order/biz/interface.go`
- Create: `internal/module/order/biz/dashboard.go`

- [ ] **Step 1: Add param/result types and interface methods**

In `internal/module/order/biz/interface.go`, add these methods to the `OrderBiz` interface (after the `// Refund` section):

```go
	// Dashboard
	GetSellerOrderStats(ctx context.Context, params GetSellerOrderStatsParams) (SellerOrderStats, error)
	GetSellerOrderTimeSeries(ctx context.Context, params GetSellerOrderTimeSeriesParams) ([]SellerOrderTimeSeriesPoint, error)
	GetSellerPendingActions(ctx context.Context, params GetSellerPendingActionsParams) (SellerPendingActions, error)
	GetSellerTopProducts(ctx context.Context, params GetSellerTopProductsParams) ([]SellerTopProduct, error)
```

- [ ] **Step 2: Create the implementation file with param structs and methods**

Create `internal/module/order/biz/dashboard.go`:

```go
package orderbiz

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// --- Param/Result structs ---

type GetSellerOrderStatsParams struct {
	SellerID  uuid.UUID `json:"seller_id" validate:"required"`
	StartDate time.Time `json:"start_date" validate:"required"`
	EndDate   time.Time `json:"end_date" validate:"required"`
}

type SellerOrderStats struct {
	TotalRevenue int64 `json:"total_revenue"`
	TotalOrders  int64 `json:"total_orders"`
	ItemsSold    int64 `json:"items_sold"`
}

type GetSellerOrderTimeSeriesParams struct {
	SellerID    uuid.UUID `json:"seller_id" validate:"required"`
	StartDate   time.Time `json:"start_date" validate:"required"`
	EndDate     time.Time `json:"end_date" validate:"required"`
	Granularity string    `json:"granularity" validate:"required,oneof=day week month"`
}

type SellerOrderTimeSeriesPoint struct {
	Date       time.Time `json:"date"`
	Revenue    int64     `json:"revenue"`
	OrderCount int64     `json:"order_count"`
}

type GetSellerPendingActionsParams struct {
	SellerID uuid.UUID `json:"seller_id" validate:"required"`
}

type SellerPendingActions struct {
	PendingItems   int64 `json:"pending_items"`
	PendingRefunds int64 `json:"pending_refunds"`
}

type GetSellerTopProductsParams struct {
	SellerID  uuid.UUID `json:"seller_id" validate:"required"`
	StartDate time.Time `json:"start_date" validate:"required"`
	EndDate   time.Time `json:"end_date" validate:"required"`
	Limit     int32     `json:"limit"`
}

type SellerTopProduct struct {
	SkuID     uuid.UUID `json:"sku_id"`
	SkuName   string    `json:"sku_name"`
	SoldCount int64     `json:"sold_count"`
	Revenue   int64     `json:"revenue"`
}

// --- Implementations ---

func (b *OrderHandler) GetSellerOrderStats(ctx restate.Context, params GetSellerOrderStatsParams) (SellerOrderStats, error) {
	row, err := b.storage.Querier().GetSellerOrderStats(ctx, orderdb.GetSellerOrderStatsParams{
		SellerID:  params.SellerID,
		StartDate: params.StartDate,
		EndDate:   params.EndDate,
	})
	if err != nil {
		return SellerOrderStats{}, sharedmodel.WrapErr("get seller order stats", err)
	}
	return SellerOrderStats{
		TotalRevenue: row.TotalRevenue,
		TotalOrders:  row.TotalOrders,
		ItemsSold:    row.ItemsSold,
	}, nil
}

func (b *OrderHandler) GetSellerOrderTimeSeries(ctx restate.Context, params GetSellerOrderTimeSeriesParams) ([]SellerOrderTimeSeriesPoint, error) {
	rows, err := b.storage.Querier().GetSellerOrderTimeSeries(ctx, orderdb.GetSellerOrderTimeSeriesParams{
		SellerID:    params.SellerID,
		Granularity: params.Granularity,
		StartDate:   params.StartDate,
		EndDate:     params.EndDate,
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("get seller order time series", err)
	}

	points := make([]SellerOrderTimeSeriesPoint, len(rows))
	for i, r := range rows {
		points[i] = SellerOrderTimeSeriesPoint{
			Date:       r.Bucket,
			Revenue:    r.Revenue,
			OrderCount: r.OrderCount,
		}
	}
	return points, nil
}

func (b *OrderHandler) GetSellerPendingActions(ctx restate.Context, params GetSellerPendingActionsParams) (SellerPendingActions, error) {
	row, err := b.storage.Querier().GetSellerPendingActions(ctx, params.SellerID)
	if err != nil {
		return SellerPendingActions{}, sharedmodel.WrapErr("get seller pending actions", err)
	}
	return SellerPendingActions{
		PendingItems:   row.PendingItems,
		PendingRefunds: row.PendingRefunds,
	}, nil
}

func (b *OrderHandler) GetSellerTopProducts(ctx restate.Context, params GetSellerTopProductsParams) ([]SellerTopProduct, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 5
	}
	rows, err := b.storage.Querier().GetSellerTopProducts(ctx, orderdb.GetSellerTopProductsParams{
		SellerID:  params.SellerID,
		StartDate: params.StartDate,
		EndDate:   params.EndDate,
		TopLimit:  limit,
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("get seller top products", err)
	}

	products := make([]SellerTopProduct, len(rows))
	for i, r := range rows {
		products[i] = SellerTopProduct{
			SkuID:     r.SkuID,
			SkuName:   r.SkuName,
			SoldCount: r.SoldCount,
			Revenue:   r.Revenue,
		}
	}
	return products, nil
}
```

- [ ] **Step 3: Regenerate Restate proxy for order module**

Run: `go generate ./internal/module/order/biz/`
Expected: `restate_gen.go` regenerated with 4 new methods

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/module/order/...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/module/order/biz/interface.go internal/module/order/biz/dashboard.go internal/module/order/biz/restate_gen.go
git commit -m "add seller dashboard methods to order module"
```

---

### Task 4: Add Dashboard Model Types to Analytic Module

**Files:**
- Create: `internal/module/analytic/model/dashboard.go`

- [ ] **Step 1: Create dashboard DTOs**

Create `internal/module/analytic/model/dashboard.go`:

```go
package analyticmodel

import (
	"time"

	"github.com/google/uuid"
)

type GetSellerDashboardParams struct {
	SellerID    uuid.UUID `json:"seller_id" validate:"required"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Granularity string    `json:"granularity" validate:"omitempty,oneof=day week month"`
}

type SellerDashboard struct {
	Summary     DashboardSummary     `json:"summary"`
	Charts      DashboardCharts      `json:"charts"`
	TopProducts []DashboardTopProduct `json:"top_products"`
}

type DashboardSummary struct {
	TotalRevenue   int64              `json:"total_revenue"`
	TotalOrders    int64              `json:"total_orders"`
	ItemsSold      int64              `json:"items_sold"`
	AverageRating  float64            `json:"average_rating"`
	PendingActions int64              `json:"pending_actions"`
	Comparison     DashboardComparison `json:"comparison"`
}

type DashboardComparison struct {
	RevenueChange  *float64 `json:"revenue_change"`
	OrdersChange   *float64 `json:"orders_change"`
	ItemsSoldChange *float64 `json:"items_sold_change"`
}

type DashboardCharts struct {
	Revenue []TimeSeriesPoint `json:"revenue"`
	Orders  []TimeSeriesPoint `json:"orders"`
}

type TimeSeriesPoint struct {
	Date  time.Time `json:"date"`
	Value int64     `json:"value"`
}

type DashboardTopProduct struct {
	SkuID     uuid.UUID `json:"sku_id"`
	SkuName   string    `json:"sku_name"`
	SoldCount int64     `json:"sold_count"`
	Revenue   int64     `json:"revenue"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/module/analytic/model/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/module/analytic/model/dashboard.go
git commit -m "add seller dashboard model types to analytic module"
```

---

### Task 5: Add GetSellerDashboard to AnalyticBiz Interface and Wire Dependencies

**Files:**
- Modify: `internal/module/analytic/biz/interface.go`
- Modify: `internal/module/analytic/fx.go`

- [ ] **Step 1: Update AnalyticBiz interface and AnalyticHandler struct**

In `internal/module/analytic/biz/interface.go`:

1. Add import for `analyticmodel`:
```go
	analyticmodel "shopnexus-server/internal/module/analytic/model"
```

2. Add import for cross-module deps:
```go
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	orderbiz "shopnexus-server/internal/module/order/biz"
```

3. Add method to `AnalyticBiz` interface:
```go
	// Dashboard
	GetSellerDashboard(ctx context.Context, params analyticmodel.GetSellerDashboardParams) (analyticmodel.SellerDashboard, error)
```

4. Add fields to `AnalyticHandler` struct:
```go
	order   orderbiz.OrderBiz
	catalog catalogbiz.CatalogBiz
```

5. Update `NewAnalyticHandler` to accept new deps:
```go
func NewAnalyticHandler(
	config *config.Config,
	storage AnalyticStorage,
	promotionBiz promotionbiz.PromotionBiz,
	orderBiz orderbiz.OrderBiz,
	catalogBiz catalogbiz.CatalogBiz,
) *AnalyticHandler {
	return &AnalyticHandler{
		storage:           storage,
		promotion:         promotionBiz,
		order:             orderBiz,
		catalog:           catalogBiz,
		popularityWeights: analyticconfig.DefaultPopularityWeights().WeightMap(),
	}
}
```

- [ ] **Step 2: Update fx.go to provide new dependencies**

In `internal/module/analytic/fx.go`, no changes needed — fx will automatically resolve `orderbiz.OrderBiz` and `catalogbiz.CatalogBiz` from the DI container since they're provided by their respective modules.

- [ ] **Step 3: Regenerate Restate proxy**

Run: `go generate ./internal/module/analytic/biz/`
Expected: `restate_gen.go` regenerated with `GetSellerDashboard` method

- [ ] **Step 4: Verify compilation (will fail — implementation not yet written)**

Run: `go build ./internal/module/analytic/biz/...`
Expected: compilation error about missing `GetSellerDashboard` method on `AnalyticHandler` — this is correct, we implement it in Task 6.

- [ ] **Step 5: Commit**

```bash
git add internal/module/analytic/biz/interface.go internal/module/analytic/biz/restate_gen.go
git commit -m "add GetSellerDashboard to analytic interface with order and catalog deps"
```

---

### Task 6: Implement GetSellerDashboard Business Logic

**Files:**
- Create: `internal/module/analytic/biz/dashboard.go`

- [ ] **Step 1: Implement the dashboard aggregation method**

Create `internal/module/analytic/biz/dashboard.go`:

```go
package analyticbiz

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	orderbiz "shopnexus-server/internal/module/order/biz"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func percentChange(current, previous int64) *float64 {
	if previous == 0 {
		return nil
	}
	change := float64(current-previous) / float64(previous) * 100
	return &change
}

func (b *AnalyticHandler) GetSellerDashboard(ctx restate.Context, params analyticmodel.GetSellerDashboardParams) (analyticmodel.SellerDashboard, error) {
	var zero analyticmodel.SellerDashboard

	// Defaults
	now := time.Now()
	if params.EndDate.IsZero() {
		params.EndDate = now
	}
	if params.StartDate.IsZero() {
		params.StartDate = params.EndDate.AddDate(0, 0, -30)
	}
	if params.Granularity == "" {
		params.Granularity = "day"
	}

	// Compute previous period
	duration := params.EndDate.Sub(params.StartDate)
	prevStart := params.StartDate.Add(-duration)
	prevEnd := params.StartDate

	// Current period stats
	currentStats, err := b.order.GetSellerOrderStats(ctx, orderbiz.GetSellerOrderStatsParams{
		SellerID:  params.SellerID,
		StartDate: params.StartDate,
		EndDate:   params.EndDate,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get current order stats", err)
	}

	// Previous period stats (for comparison)
	prevStats, err := b.order.GetSellerOrderStats(ctx, orderbiz.GetSellerOrderStatsParams{
		SellerID:  params.SellerID,
		StartDate: prevStart,
		EndDate:   prevEnd,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get previous order stats", err)
	}

	// Time series
	timeSeries, err := b.order.GetSellerOrderTimeSeries(ctx, orderbiz.GetSellerOrderTimeSeriesParams{
		SellerID:    params.SellerID,
		StartDate:   params.StartDate,
		EndDate:     params.EndDate,
		Granularity: params.Granularity,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get order time series", err)
	}

	// Pending actions
	pending, err := b.order.GetSellerPendingActions(ctx, orderbiz.GetSellerPendingActionsParams{
		SellerID: params.SellerID,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get pending actions", err)
	}

	// Top products
	topProducts, err := b.order.GetSellerTopProducts(ctx, orderbiz.GetSellerTopProductsParams{
		SellerID:  params.SellerID,
		StartDate: params.StartDate,
		EndDate:   params.EndDate,
		Limit:     5,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get top products", err)
	}

	// Average rating from catalog
	vendorStats, err := b.catalog.GetVendorStats(ctx, catalogbiz.GetVendorStatsParams{
		AccountID: params.SellerID,
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get vendor stats", err)
	}

	// Build charts
	revenuePoints := make([]analyticmodel.TimeSeriesPoint, len(timeSeries))
	orderPoints := make([]analyticmodel.TimeSeriesPoint, len(timeSeries))
	for i, ts := range timeSeries {
		revenuePoints[i] = analyticmodel.TimeSeriesPoint{Date: ts.Date, Value: ts.Revenue}
		orderPoints[i] = analyticmodel.TimeSeriesPoint{Date: ts.Date, Value: ts.OrderCount}
	}

	// Build top products
	dashTopProducts := make([]analyticmodel.DashboardTopProduct, len(topProducts))
	for i, tp := range topProducts {
		dashTopProducts[i] = analyticmodel.DashboardTopProduct{
			SkuID:     tp.SkuID,
			SkuName:   tp.SkuName,
			SoldCount: tp.SoldCount,
			Revenue:   tp.Revenue,
		}
	}

	return analyticmodel.SellerDashboard{
		Summary: analyticmodel.DashboardSummary{
			TotalRevenue:   currentStats.TotalRevenue,
			TotalOrders:    currentStats.TotalOrders,
			ItemsSold:      currentStats.ItemsSold,
			AverageRating:  vendorStats.AverageRating,
			PendingActions: pending.PendingItems + pending.PendingRefunds,
			Comparison: analyticmodel.DashboardComparison{
				RevenueChange:   percentChange(currentStats.TotalRevenue, prevStats.TotalRevenue),
				OrdersChange:    percentChange(currentStats.TotalOrders, prevStats.TotalOrders),
				ItemsSoldChange: percentChange(currentStats.ItemsSold, prevStats.ItemsSold),
			},
		},
		Charts: analyticmodel.DashboardCharts{
			Revenue: revenuePoints,
			Orders:  orderPoints,
		},
		TopProducts: dashTopProducts,
	}, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/module/analytic/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/module/analytic/biz/dashboard.go
git commit -m "implement GetSellerDashboard aggregation logic"
```

---

### Task 7: Add HTTP Transport Handler for Dashboard Endpoint

**Files:**
- Modify: `internal/module/analytic/transport/echo/analytic.go`

- [ ] **Step 1: Add the route and handler**

In `internal/module/analytic/transport/echo/analytic.go`:

1. Add imports:
```go
	"time"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	authclaims "shopnexus-server/internal/shared/claims"
```

2. Add route in `NewHandler` (after the existing routes):
```go
	api.GET("/seller-dashboard", h.GetSellerDashboard)
```

3. Add handler method:

```go
type GetSellerDashboardRequest struct {
	Start       string `query:"start"`
	End         string `query:"end"`
	Granularity string `query:"granularity"`
}

func (h *Handler) GetSellerDashboard(c echo.Context) error {
	var req GetSellerDashboardRequest
	if err := c.Bind(&req); err != nil {
		return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
	}

	claims, err := authclaims.GetClaims(c.Request())
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusUnauthorized, err)
	}

	params := analyticmodel.GetSellerDashboardParams{
		SellerID:    claims.Account.ID,
		Granularity: req.Granularity,
	}

	if req.Start != "" {
		t, err := time.Parse(time.RFC3339, req.Start)
		if err != nil {
			return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
		}
		params.StartDate = t
	}
	if req.End != "" {
		t, err := time.Parse(time.RFC3339, req.End)
		if err != nil {
			return response.FromError(c.Response().Writer, http.StatusBadRequest, err)
		}
		params.EndDate = t
	}

	result, err := h.biz.GetSellerDashboard(c.Request().Context(), params)
	if err != nil {
		return response.FromError(c.Response().Writer, http.StatusInternalServerError, err)
	}

	return response.FromDTO(c.Response().Writer, http.StatusOK, result)
}
```

- [ ] **Step 2: Verify full backend compilation**

Run: `go build ./cmd/server/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/module/analytic/transport/echo/analytic.go
git commit -m "add seller-dashboard HTTP endpoint"
```

---

### Task 8: Install Frontend Dependencies

**Files:**
- Modify: `../shopnexus-website-v3/package.json`

- [ ] **Step 1: Install recharts and date-fns**

Run from `../shopnexus-website-v3/`:

```bash
cd ../shopnexus-website-v3 && bun add recharts date-fns
```

Expected: packages installed, `package.json` and `bun.lock` updated

- [ ] **Step 2: Verify the app still builds**

Run: `cd ../shopnexus-website-v3 && bun run build`
Expected: build succeeds (or at minimum no new errors)

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3 && git add package.json bun.lock && git commit -m "add recharts and date-fns dependencies"
```

---

### Task 9: Create Dashboard API Hook

**Files:**
- Create: `../shopnexus-website-v3/core/analytic/dashboard.ts`

- [ ] **Step 1: Create the types and hook**

Create `../shopnexus-website-v3/core/analytic/dashboard.ts`:

```typescript
import { customFetchStandard } from "@/lib/queryclient/custom-fetch"
import { useQuery } from "@tanstack/react-query"

// ===== Types =====

export type DashboardComparison = {
  revenue_change: number | null
  orders_change: number | null
  items_sold_change: number | null
}

export type DashboardSummary = {
  total_revenue: number
  total_orders: number
  items_sold: number
  average_rating: number
  pending_actions: number
  comparison: DashboardComparison
}

export type TimeSeriesPoint = {
  date: string
  value: number
}

export type DashboardCharts = {
  revenue: TimeSeriesPoint[]
  orders: TimeSeriesPoint[]
}

export type DashboardTopProduct = {
  sku_id: string
  sku_name: string
  sold_count: number
  revenue: number
}

export type SellerDashboard = {
  summary: DashboardSummary
  charts: DashboardCharts
  top_products: DashboardTopProduct[]
}

// ===== Hooks =====

export type UseSellerDashboardParams = {
  start?: string // RFC3339
  end?: string   // RFC3339
  granularity?: 'day' | 'week' | 'month'
}

export const useSellerDashboard = (params: UseSellerDashboardParams = {}) => {
  const searchParams = new URLSearchParams()
  if (params.start) searchParams.set('start', params.start)
  if (params.end) searchParams.set('end', params.end)
  if (params.granularity) searchParams.set('granularity', params.granularity)

  const queryString = searchParams.toString()
  const url = `analytic/seller-dashboard${queryString ? `?${queryString}` : ''}`

  return useQuery({
    queryKey: ['seller-dashboard', params.start, params.end, params.granularity],
    queryFn: () => customFetchStandard<SellerDashboard>(url),
  })
}
```

- [ ] **Step 2: Verify TypeScript compilation**

Run: `cd ../shopnexus-website-v3 && npx tsc --noEmit --pretty core/analytic/dashboard.ts`
Expected: no type errors

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3 && git add core/analytic/dashboard.ts && git commit -m "add seller dashboard API hook and types"
```

---

### Task 10: Rewrite Seller Dashboard Page

**Files:**
- Modify: `../shopnexus-website-v3/app/(marketplace)/seller/page.tsx`

- [ ] **Step 1: Rewrite the page with real data**

Replace the entire contents of `../shopnexus-website-v3/app/(marketplace)/seller/page.tsx`:

```tsx
"use client"

import { useState, useMemo } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  DollarSign,
  ShoppingCart,
  Package,
  Star,
  AlertCircle,
  ArrowUpRight,
  ArrowDownRight,
  CalendarIcon,
} from "lucide-react"
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from "recharts"
import { format, subDays, subMonths, startOfMonth, startOfYear } from "date-fns"
import {
  useSellerDashboard,
  type UseSellerDashboardParams,
} from "@/core/analytic/dashboard"
import { formatPrice } from "@/lib/utils"
import Link from "next/link"

// ===== Date Range Presets =====

type DatePreset = {
  label: string
  getRange: () => { start: Date; end: Date }
}

const DATE_PRESETS: DatePreset[] = [
  {
    label: "Last 7 days",
    getRange: () => ({ start: subDays(new Date(), 7), end: new Date() }),
  },
  {
    label: "Last 30 days",
    getRange: () => ({ start: subDays(new Date(), 30), end: new Date() }),
  },
  {
    label: "This month",
    getRange: () => ({ start: startOfMonth(new Date()), end: new Date() }),
  },
  {
    label: "Last 3 months",
    getRange: () => ({ start: subMonths(new Date(), 3), end: new Date() }),
  },
  {
    label: "This year",
    getRange: () => ({ start: startOfYear(new Date()), end: new Date() }),
  },
]

const GRANULARITY_OPTIONS = ["day", "week", "month"] as const

function inferGranularity(start: Date, end: Date): "day" | "week" | "month" {
  const days = (end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24)
  if (days <= 31) return "day"
  if (days <= 180) return "week"
  return "month"
}

// ===== Components =====

function StatCard({
  title,
  value,
  change,
  icon: Icon,
  format: formatFn,
}: {
  title: string
  value: number
  change: number | null
  icon: React.ElementType
  format?: (v: number) => string
}) {
  const displayValue = formatFn ? formatFn(value) : value.toLocaleString()
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {title}
        </CardTitle>
        <Icon className="h-4 w-4 text-muted-foreground" />
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{displayValue}</div>
        {change !== null ? (
          <div className="flex items-center text-xs">
            {change >= 0 ? (
              <ArrowUpRight className="h-3 w-3 text-green-500 mr-1" />
            ) : (
              <ArrowDownRight className="h-3 w-3 text-red-500 mr-1" />
            )}
            <span className={change >= 0 ? "text-green-500" : "text-red-500"}>
              {change >= 0 ? "+" : ""}
              {change.toFixed(1)}%
            </span>
            <span className="text-muted-foreground ml-1">vs prev period</span>
          </div>
        ) : (
          <div className="text-xs text-muted-foreground">N/A</div>
        )}
      </CardContent>
    </Card>
  )
}

function DashboardChart({
  title,
  data,
  granularity,
  onGranularityChange,
  formatValue,
  allowedGranularities,
}: {
  title: string
  data: { date: string; value: number }[]
  granularity: string
  onGranularityChange: (g: "day" | "week" | "month") => void
  formatValue?: (v: number) => string
  allowedGranularities: readonly ("day" | "week" | "month")[]
}) {
  const chartData = data.map((d) => ({
    date: format(new Date(d.date), granularity === "day" ? "MMM d" : granularity === "week" ? "MMM d" : "MMM yyyy"),
    value: d.value,
  }))

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>{title}</CardTitle>
        <div className="flex gap-1">
          {GRANULARITY_OPTIONS.map((g) => (
            <Button
              key={g}
              variant={granularity === g ? "default" : "outline"}
              size="sm"
              className="text-xs h-7"
              onClick={() => onGranularityChange(g)}
              disabled={!allowedGranularities.includes(g)}
            >
              {g.charAt(0).toUpperCase() + g.slice(1)}
            </Button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        {chartData.length === 0 ? (
          <div className="flex items-center justify-center h-[250px] text-muted-foreground">
            No data for this period
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={250}>
            <LineChart data={chartData}>
              <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
              <XAxis dataKey="date" className="text-xs" tick={{ fontSize: 12 }} />
              <YAxis className="text-xs" tick={{ fontSize: 12 }} tickFormatter={formatValue} />
              <Tooltip
                formatter={(value: number) =>
                  formatValue ? [formatValue(value), title] : [value.toLocaleString(), title]
                }
              />
              <Line
                type="monotone"
                dataKey="value"
                stroke="hsl(var(--primary))"
                strokeWidth={2}
                dot={false}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}

function DashboardSkeleton() {
  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 5 }).map((_, i) => (
          <Card key={i}>
            <CardHeader className="pb-2">
              <Skeleton className="h-4 w-24" />
            </CardHeader>
            <CardContent>
              <Skeleton className="h-8 w-32 mb-2" />
              <Skeleton className="h-3 w-20" />
            </CardContent>
          </Card>
        ))}
      </div>
      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardContent className="pt-6">
            <Skeleton className="h-[250px] w-full" />
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <Skeleton className="h-[250px] w-full" />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

// ===== Page =====

export default function SellerDashboardPage() {
  const [activePreset, setActivePreset] = useState(1) // "Last 30 days"
  const [customRange, setCustomRange] = useState<{
    start: string
    end: string
  } | null>(null)
  const [granularity, setGranularity] = useState<"day" | "week" | "month">("day")

  const dateRange = useMemo(() => {
    if (customRange) {
      return { start: new Date(customRange.start), end: new Date(customRange.end) }
    }
    return DATE_PRESETS[activePreset].getRange()
  }, [activePreset, customRange])

  const allowedGranularities = useMemo(() => {
    const days = (dateRange.end.getTime() - dateRange.start.getTime()) / (1000 * 60 * 60 * 24)
    if (days <= 31) return ["day", "week", "month"] as const
    if (days <= 180) return ["week", "month"] as const
    return ["week", "month"] as const
  }, [dateRange])

  const params: UseSellerDashboardParams = useMemo(() => ({
    start: dateRange.start.toISOString(),
    end: dateRange.end.toISOString(),
    granularity,
  }), [dateRange, granularity])

  const { data, isLoading } = useSellerDashboard(params)

  const handlePresetSelect = (index: number) => {
    setActivePreset(index)
    setCustomRange(null)
    const range = DATE_PRESETS[index].getRange()
    setGranularity(inferGranularity(range.start, range.end))
  }

  const handleCustomRange = (start: string, end: string) => {
    setCustomRange({ start, end })
    setGranularity(inferGranularity(new Date(start), new Date(end)))
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold">Dashboard</h1>
          <p className="text-muted-foreground">Overview of your store performance</p>
        </div>
        <DashboardSkeleton />
      </div>
    )
  }

  const summary = data?.summary
  const charts = data?.charts
  const topProducts = data?.top_products ?? []

  return (
    <div className="space-y-6">
      {/* Header with Date Picker */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold">Dashboard</h1>
          <p className="text-muted-foreground">Overview of your store performance</p>
        </div>
        <div className="flex items-center gap-2 flex-wrap">
          {DATE_PRESETS.map((preset, i) => (
            <Button
              key={preset.label}
              variant={activePreset === i && !customRange ? "default" : "outline"}
              size="sm"
              onClick={() => handlePresetSelect(i)}
            >
              {preset.label}
            </Button>
          ))}
          <Popover>
            <PopoverTrigger asChild>
              <Button variant={customRange ? "default" : "outline"} size="sm">
                <CalendarIcon className="h-4 w-4 mr-1" />
                Custom
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-auto p-4" align="end">
              <div className="flex flex-col gap-2">
                <label className="text-sm font-medium">Start</label>
                <input
                  type="date"
                  className="border rounded px-2 py-1 text-sm"
                  defaultValue={customRange?.start?.slice(0, 10) ?? ""}
                  onChange={(e) => {
                    const end = customRange?.end ?? new Date().toISOString()
                    handleCustomRange(new Date(e.target.value).toISOString(), end)
                  }}
                />
                <label className="text-sm font-medium">End</label>
                <input
                  type="date"
                  className="border rounded px-2 py-1 text-sm"
                  defaultValue={customRange?.end?.slice(0, 10) ?? ""}
                  onChange={(e) => {
                    const start = customRange?.start ?? subDays(new Date(), 30).toISOString()
                    handleCustomRange(start, new Date(e.target.value).toISOString())
                  }}
                />
              </div>
            </PopoverContent>
          </Popover>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
        <StatCard
          title="Total Revenue"
          value={summary?.total_revenue ?? 0}
          change={summary?.comparison.revenue_change ?? null}
          icon={DollarSign}
          format={formatPrice}
        />
        <StatCard
          title="Total Orders"
          value={summary?.total_orders ?? 0}
          change={summary?.comparison.orders_change ?? null}
          icon={ShoppingCart}
        />
        <StatCard
          title="Items Sold"
          value={summary?.items_sold ?? 0}
          change={summary?.comparison.items_sold_change ?? null}
          icon={Package}
        />
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Avg Rating
            </CardTitle>
            <Star className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {summary?.average_rating?.toFixed(1) ?? "0.0"}
            </div>
            <div className="text-xs text-muted-foreground">across all products</div>
          </CardContent>
        </Card>
        {(summary?.pending_actions ?? 0) > 0 && (
          <Card className="border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950">
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardTitle className="text-sm font-medium text-amber-700 dark:text-amber-300">
                Pending Actions
              </CardTitle>
              <AlertCircle className="h-4 w-4 text-amber-600" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-amber-700 dark:text-amber-300">
                {summary?.pending_actions ?? 0}
              </div>
              <div className="flex gap-2 text-xs">
                <Link
                  href="/seller/incoming"
                  className="text-amber-600 underline hover:text-amber-800"
                >
                  Incoming items
                </Link>
                <Link
                  href="/seller/refunds"
                  className="text-amber-600 underline hover:text-amber-800"
                >
                  Refunds
                </Link>
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      {/* Charts */}
      <div className="grid gap-6 lg:grid-cols-2">
        <DashboardChart
          title="Revenue"
          data={charts?.revenue ?? []}
          granularity={granularity}
          onGranularityChange={setGranularity}
          formatValue={formatPrice}
          allowedGranularities={allowedGranularities}
        />
        <DashboardChart
          title="Orders"
          data={charts?.orders ?? []}
          granularity={granularity}
          onGranularityChange={setGranularity}
          allowedGranularities={allowedGranularities}
        />
      </div>

      {/* Top Products */}
      <Card>
        <CardHeader>
          <CardTitle>Top Products</CardTitle>
        </CardHeader>
        <CardContent>
          {topProducts.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No product sales in this period
            </div>
          ) : (
            <div className="space-y-4">
              {topProducts.map((product, index) => (
                <div
                  key={product.sku_id}
                  className="flex items-center justify-between py-2 border-b last:border-0"
                >
                  <div className="flex items-center gap-3">
                    <span className="flex h-8 w-8 items-center justify-center rounded-full bg-muted text-sm font-medium">
                      {index + 1}
                    </span>
                    <div>
                      <p className="font-medium">{product.sku_name}</p>
                      <p className="text-sm text-muted-foreground">
                        {product.sold_count} sold
                      </p>
                    </div>
                  </div>
                  <p className="font-medium">{formatPrice(product.revenue)}</p>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
```

- [ ] **Step 2: Verify the page compiles**

Run: `cd ../shopnexus-website-v3 && npx tsc --noEmit`
Expected: no type errors (or only pre-existing ones)

- [ ] **Step 3: Commit**

```bash
cd ../shopnexus-website-v3 && git add app/\(marketplace\)/seller/page.tsx && git commit -m "rewrite seller dashboard with real data, charts, and date filtering"
```

---

### Task 11: Add Frontend Analytics Event Tracking

**Files:**
- Modify: `../shopnexus-website-v3/core/analytic/analytic.ts`

- [ ] **Step 1: Add new event types to the union**

In `../shopnexus-website-v3/core/analytic/analytic.ts`, update the `event_type` union in `useCreateInteraction`:

```typescript
event_type: 'view' | 'add_to_cart' | 'purchase' | 'rating' | 'product_impression' | 'checkout_started'
```

- [ ] **Step 2: Commit**

```bash
cd ../shopnexus-website-v3 && git add core/analytic/analytic.ts && git commit -m "add product_impression and checkout_started event types"
```

---

### Task 12: Final Verification

- [ ] **Step 1: Verify full backend builds**

Run: `go build ./cmd/server/`
Expected: no errors

- [ ] **Step 2: Verify frontend builds**

Run: `cd ../shopnexus-website-v3 && bun run build`
Expected: build succeeds

- [ ] **Step 3: Run backend codegen to ensure everything is in sync**

Run: `make generate && sqlc generate`
Expected: no changes needed (all generated files already up to date)

- [ ] **Step 4: Review SQL queries make sense**

Manually verify that the SQL in `internal/module/order/db/queries/dashboard_custom.sql` matches the actual table/column names from `internal/module/order/db/migrations/0001_init.up.sql`.

Key column mappings:
- `order.order.seller_id` exists
- `order.order.status` uses enum `'Success'`
- `order.order.total` is `BIGINT`
- `order.order.date_created` is `TIMESTAMPTZ`
- `order.item.order_id`, `order.item.quantity`, `order.item.paid_amount`, `order.item.sku_id`, `order.item.sku_name`, `order.item.status` all exist
- `order.refund.status` uses enum from `order.status`

# Seller Dashboard — Design Spec

## Overview

Enrich the analytic module with a seller dashboard API endpoint and wire it into the existing seller dashboard page in the frontend. Replace all hardcoded mock data with real aggregated data from orders, catalog, inventory, and analytics.

## Decisions

- **Backend location:** Extend the existing `analytic` module (not a new module)
- **API shape:** Single endpoint returning all dashboard data in one response
- **Chart library:** Recharts (frontend)
- **Date range:** Preset quick picks + custom date picker
- **New analytics events:** `product_impression` and `checkout_started`

---

## Backend API

### Endpoint

`GET /api/v1/analytic/seller-dashboard`

Seller identified from auth context.

### Query Parameters

| Param | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `start` | RFC3339 | No | 30 days ago | Range start |
| `end` | RFC3339 | No | now | Range end |
| `granularity` | `day`\|`week`\|`month` | No | `day` | Chart bucket size |

### Response

```json
{
  "summary": {
    "total_revenue": 1250000,
    "total_orders": 84,
    "items_sold": 142,
    "average_rating": 4.3,
    "pending_actions": 7,
    "comparison": {
      "revenue_change": 12.5,
      "orders_change": -3.2,
      "items_sold_change": 8.1
    }
  },
  "charts": {
    "revenue": [
      { "date": "2026-03-01", "value": 45000 }
    ],
    "orders": [
      { "date": "2026-03-01", "value": 3 }
    ]
  },
  "top_products": [
    {
      "spu_id": "123",
      "name": "Product Name",
      "sold_count": 42,
      "revenue": 420000
    }
  ]
}
```

### Summary Cards (5)

1. **Total Revenue** — sum of completed order totals for the seller in the period, with % change vs previous period
2. **Total Orders** — count of orders, with % change
3. **Items Sold** — total item quantity across orders, with % change
4. **Average Rating** — current average across all seller's products (no comparison)
5. **Pending Actions** — count of unconfirmed incoming items + pending refunds (no comparison)

### Comparison Logic

The `_change` fields are percentage change vs. the equivalent previous period. If the selected range is 30 days, comparison is against the 30 days before that. Formula: `((current - previous) / previous) * 100`. If previous is 0, return `null`.

### Charts

- **Revenue over time** — line chart, bucketed by granularity
- **Orders over time** — line chart, bucketed by granularity

### Top Products

Ranked by sold count within the selected period. Limited to top 5. Includes product name and revenue per product.

---

## Data Aggregation

### New Method on AnalyticBiz

`GetSellerDashboard(ctx restate.Context, params GetSellerDashboardParams) (SellerDashboard, error)`

### Cross-Module Dependencies (via Restate proxies in fx.go)

- **OrderBiz** — seller order stats, time series, pending actions, top products
- **CatalogBiz** — average rating, product names
- **InventoryBiz** — sold counts per SKU (for top products)

### New SQL Queries (order module, exposed via OrderBiz)

- `GetSellerOrderStats(seller_id, start, end)` — revenue sum, order count, items sold
- `GetSellerOrderTimeSeries(seller_id, start, end, granularity)` — date-bucketed revenue + order counts
- `GetSellerPendingActions(seller_id)` — unconfirmed items + pending refunds count
- `GetSellerTopProducts(seller_id, start, end, limit)` — top products by sold count with revenue

### Aggregation Flow

1. Parse `start`/`end`/`granularity` from query params
2. Compute previous period (same duration, shifted back)
3. Call cross-module methods (parallel where possible):
   - Orders current period -> revenue, order count, items sold
   - Orders previous period -> comparison deltas
   - Catalog -> average rating
   - Orders -> top products by sold count + revenue
   - Orders -> pending incoming items + pending refunds
4. Bucket revenue/orders into time-series bins
5. Assemble and return response

---

## New Analytics Events

### Constants (in `analytic/model/analytic.go`)

```go
EventProductImpression = "product_impression"  // product appears in search/category listings
EventCheckoutStarted   = "checkout_started"    // buyer begins checkout
```

### Frontend Tracking

- `product_impression` — fired from product list/search components when products render into view
- `checkout_started` — fired from checkout page on mount

### Popularity Handler

Add weight config entries for `product_impression` and `checkout_started` in the popularity scoring logic.

---

## Frontend

### Dependencies

- `recharts` — install via `bun add recharts`

### API Hook

New file: `core/analytic/dashboard.ts`

- `useSellerDashboard({ start, end, granularity })` — single query hook fetching the endpoint
- Typed `SellerDashboard` response interface

### Page Rewrite

File: `app/(marketplace)/seller/page.tsx`

Replace all hardcoded mock data. Layout:

```
┌─────────────────────────────────────────────────┐
│  Date Range Picker (presets + custom)            │
├──────────┬──────────┬──────────┬────────────────┤
│ Revenue  │ Orders   │ Items    │ Avg Rating     │
│ +12.5%   │ -3.2%    │ Sold     │                │
│          │          │ +8.1%    │                │
├──────────┴──────────┴──────────┴────────────────┤
│          Pending Actions Banner (if > 0)         │
├─────────────────────────────────────────────────┤
│  Revenue Chart (line)  │  Orders Chart (line)    │
│  [day/week/month tabs] │  [day/week/month tabs]  │
├─────────────────────────────────────────────────┤
│  Top Products Table                              │
│  #  Name  Sold  Revenue                          │
└─────────────────────────────────────────────────┘
```

### Components

- **DateRangePicker** — presets ("Last 7 days", "Last 30 days", "This month", "This year") + custom date picker. Controls `start`/`end` state.
- **StatCard** — metric value + percentage change badge (green/red). Uses shadcn Card.
- **PendingActionsBanner** — clickable, links to `/seller/incoming` and `/seller/refunds`.
- **DashboardChart** — wraps Recharts `LineChart` with granularity tabs. Reused for revenue and orders.
- **TopProductsTable** — ranked table: product name, sold count, revenue.

### Loading State

Skeleton cards and chart placeholders using shadcn `Skeleton`.

---

## Error Handling & Edge Cases

### New Seller (No Data)

- Summary cards: show 0 values, comparison shows "N/A"
- Charts: empty state with "No data for this period"
- Top products: empty state

### Cross-Module Failures

- Restate retries automatically on transient failures
- If a module is genuinely down, return partial data with null for failed sections

### Date Range Validation

- `end` must be after `start`
- Max range: 1 year
- Default: last 30 days, daily granularity

### Granularity Auto-Adjustment

- Range <= 31 days: allow `day`
- Range <= 6 months: allow `day`, `week`
- Range > 6 months: allow `week`, `month`

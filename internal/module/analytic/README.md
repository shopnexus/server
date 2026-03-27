# Analytic Module

Tracks user interactions and computes real-time product popularity scores. Interactions are recorded via Restate durable execution, then fanned out to popularity scoring and catalog recommendation updates using fire-and-forget calls (`restate.ServiceSend`).

**Struct**: `AnalyticHandler` | **Interface**: `AnalyticBiz` | **Restate service**: `Analytic`

---

## How It Works

1. **CreateInteraction** batch-inserts interaction rows into PostgreSQL.
2. For each inserted row, fires two durable Restate calls:
   - `Analytic.HandlePopularityEvent` -- updates the product popularity score.
   - `Catalog.AddInteraction` -- feeds the recommendation engine.
3. **HandlePopularityEvent** filters for product-scoped events, looks up the event weight, and performs an atomic `INSERT ... ON CONFLICT DO UPDATE` to accumulate the score and increment counters. No read-before-write -- PostgreSQL handles the atomic increment.

---

## Database Tables

All tables in the `analytic` schema.

| Table | Key Columns | Notes |
|-------|-------------|-------|
| `interaction` | id, account_id, account_number, event_type, ref_type (Product/Category/Brand), ref_id, metadata (JSONB) | Indexed on account, session, event type, (ref_type, ref_id) |
| `product_popularity` | spu_id (PK), score, view_count, purchase_count, favorite_count, cart_count, review_count | Descending index on score for top-N queries |

---

## Event Weight Table

Weights are configurable via `PopularityWeights` config. Defaults:

| Event Type | Weight | Counter |
|------------|--------|---------|
| `purchase` | +0.80 | purchase_count |
| `add_to_favorites` | +0.60 | favorite_count |
| `add_to_cart` | +0.50 | cart_count |
| `write_review` | +0.50 | review_count |
| `rating_high` | +0.40 | review_count |
| `view` | +0.30 | view_count |
| `ask_question` | +0.25 | -- |
| `click_from_search` | +0.20 | -- |
| `click_from_recommendation` | +0.15 | -- |
| `view_similar_products` | +0.15 | -- |
| `click_from_category` | +0.12 | -- |
| `rating_medium` | +0.10 | review_count |
| `view_bounce` | -0.10 | view_count |
| `not_interested` | -0.30 | -- |
| `remove_from_cart` | -0.30 | cart_count |
| `hide_item` | -0.35 | -- |
| `rating_low` | -0.50 | review_count |
| `dislike` | -0.50 | -- |
| `return_product` | -0.60 | -- |
| `cancel_order` | -0.60 | -- |
| `refund_requested` | -0.70 | -- |
| `report_product` | -1.20 | -- |

Events not in the weight map are silently skipped.

---

## Endpoints

All routes prefixed with `/api/v1/analytic`.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/interaction` | Yes | Record one or more interaction events |
| GET | `/popularity/top` | No | List top products by popularity score (paginated) |
| GET | `/popularity/:spu_id` | No | Get popularity data for a specific product |

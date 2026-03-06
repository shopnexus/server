# Analytics Module

## Overview

The Analytics module is responsible for tracking user interactions across the ShopNexus e-commerce platform and computing real-time product popularity scores. It operates as an event-driven subsystem: incoming interactions are persisted to PostgreSQL, published to a Kafka topic via Watermill, and consumed asynchronously to maintain an incrementally updated popularity leaderboard.

Core capabilities:

- **Interaction tracking** -- records every user event (views, purchases, cart changes, reviews, etc.) against a product, category, or brand reference.
- **Product popularity scoring** -- assigns configurable weights to each event type and maintains a running score per SPU (Standard Product Unit) through an upsert-based accumulation strategy.
- **Event-driven pipeline** -- uses Kafka (via the internal `pubsub.Client` abstraction over Watermill) to decouple interaction persistence from popularity computation.
- **Batch and bulk insert support** -- supports pgx batch inserts and `COPY FROM` for high-throughput ingestion scenarios.

The module is wired into the application through Uber fx dependency injection and exposes a RESTful API via Echo v4.

---
## Database Schema

All tables reside in the `analytic` PostgreSQL schema.

### Table: `analytic.interaction`

Stores every user interaction event.

```sql
CREATE SCHEMA IF NOT EXISTS "analytic";

CREATE TYPE "analytic"."interaction_ref_type" AS ENUM ('Product', 'Category', 'Brand');

CREATE TABLE IF NOT EXISTS "analytic"."interaction" (
    "id"             BIGSERIAL NOT NULL,
    "account_id"     UUID,
    "session_id"     TEXT,
    "event_type"     TEXT NOT NULL,
    "ref_type"       "analytic"."interaction_ref_type" NOT NULL,
    "ref_id"         TEXT NOT NULL,
    "metadata"       JSONB,
    "user_agent"     TEXT,
    "ip_address"     TEXT,
    "date_created"   TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "account_number" BIGINT NOT NULL,
    CONSTRAINT "interaction_pkey" PRIMARY KEY ("id")
);
```

| Column           | Type                              | Description                                                     |
|------------------|-----------------------------------|-----------------------------------------------------------------|
| `id`             | `BIGSERIAL` (PK)                  | Auto-incrementing unique identifier                             |
| `account_id`     | `UUID` (nullable)                 | The authenticated user's UUID, null for anonymous sessions      |
| `account_number` | `BIGINT`                          | Numeric account identifier                                      |
| `session_id`     | `TEXT` (nullable)                 | Browser/client session identifier                               |
| `event_type`     | `TEXT`                            | The type of event (see Event Types below)                       |
| `ref_type`       | `ENUM('Product','Category','Brand')` | What entity the interaction references                       |
| `ref_id`         | `TEXT`                            | The ID of the referenced entity (e.g., SPU UUID)                |
| `metadata`       | `JSONB` (nullable)                | Arbitrary JSON payload for additional event context             |
| `user_agent`     | `TEXT` (nullable)                 | HTTP User-Agent string                                          |
| `ip_address`     | `TEXT` (nullable)                 | Client IP address                                               |
| `date_created`   | `TIMESTAMPTZ(3)`                  | Timestamp with millisecond precision, defaults to `NOW()`       |

**Indexes:**

```sql
CREATE INDEX "interaction_account_id_idx"      ON "analytic"."interaction" ("account_id");
CREATE INDEX "interaction_session_id_idx"      ON "analytic"."interaction" ("session_id");
CREATE INDEX "interaction_event_type_idx"      ON "analytic"."interaction" ("event_type");
CREATE INDEX "interaction_ref_type_ref_id_idx" ON "analytic"."interaction" ("ref_type", "ref_id");
```

| Index Name                          | Columns                  | Purpose                                        |
|-------------------------------------|--------------------------|-------------------------------------------------|
| `interaction_account_id_idx`        | `account_id`             | Fast lookups by user                            |
| `interaction_session_id_idx`        | `session_id`             | Fast lookups by session                         |
| `interaction_event_type_idx`        | `event_type`             | Filter interactions by event type               |
| `interaction_ref_type_ref_id_idx`   | `ref_type`, `ref_id`     | Composite index for entity-specific queries     |

### Table: `analytic.product_popularity`

Maintains an aggregated popularity score and per-metric counters for each product (SPU).

```sql
CREATE TABLE IF NOT EXISTS "analytic"."product_popularity" (
    "spu_id"         UUID NOT NULL,
    "score"          DOUBLE PRECISION NOT NULL DEFAULT 0,
    "view_count"     BIGINT NOT NULL DEFAULT 0,
    "purchase_count" BIGINT NOT NULL DEFAULT 0,
    "favorite_count" BIGINT NOT NULL DEFAULT 0,
    "cart_count"     BIGINT NOT NULL DEFAULT 0,
    "review_count"   BIGINT NOT NULL DEFAULT 0,
    "date_updated"   TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "product_popularity_pkey" PRIMARY KEY ("spu_id")
);

CREATE INDEX "product_popularity_score_idx" ON "analytic"."product_popularity" ("score" DESC);
```

| Column           | Type               | Description                                                   |
|------------------|--------------------|---------------------------------------------------------------|
| `spu_id`         | `UUID` (PK)        | The Standard Product Unit identifier                          |
| `score`          | `DOUBLE PRECISION` | Cumulative weighted popularity score                          |
| `view_count`     | `BIGINT`           | Total view events                                             |
| `purchase_count` | `BIGINT`           | Total purchase events                                         |
| `favorite_count` | `BIGINT`           | Total add-to-favorites events                                 |
| `cart_count`     | `BIGINT`           | Total cart-related events (add + remove)                      |
| `review_count`   | `BIGINT`           | Total review/rating events                                    |
| `date_updated`   | `TIMESTAMPTZ(3)`   | Timestamp of the most recent popularity update                |

The `product_popularity_score_idx` descending index supports efficient "top N popular products" queries.

**Upsert strategy:**

The `UpsertProductPopularity` query uses `ON CONFLICT ... DO UPDATE` to atomically increment counters and accumulate the score without requiring a separate read:

```sql
INSERT INTO "analytic"."product_popularity" (...)
VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
ON CONFLICT ("spu_id") DO UPDATE
SET "score"          = "analytic"."product_popularity"."score"          + EXCLUDED."score",
    "view_count"     = "analytic"."product_popularity"."view_count"     + EXCLUDED."view_count",
    "purchase_count" = "analytic"."product_popularity"."purchase_count" + EXCLUDED."purchase_count",
    "favorite_count" = "analytic"."product_popularity"."favorite_count" + EXCLUDED."favorite_count",
    "cart_count"     = "analytic"."product_popularity"."cart_count"     + EXCLUDED."cart_count",
    "review_count"   = "analytic"."product_popularity"."review_count"   + EXCLUDED."review_count",
    "date_updated"   = CURRENT_TIMESTAMP
RETURNING *;
```

---

## API Endpoints

All endpoints are registered under `/api/v1/analytic`.

| Method | Path                            | Handler                    | Auth Required | Description                                  |
|--------|---------------------------------|----------------------------|---------------|----------------------------------------------|
| POST   | `/api/v1/analytic/interaction`  | `CreateInteraction`        | Yes           | Record one or more interaction events        |
| GET    | `/api/v1/analytic/popularity/top` | `ListTopProductPopularity` | No          | Retrieve top products ranked by popularity   |
| GET    | `/api/v1/analytic/popularity/:spu_id` | `GetProductPopularity` | No          | Get popularity data for a specific product   |

### POST `/api/v1/analytic/interaction`

Records one or more user interaction events in a single request. Requires an authenticated session (JWT claims are extracted from the request).

**Request body:**

```json
{
  "interactions": [
    {
      "event_type": "view",
      "ref_type": "Product",
      "ref_id": "550e8400-e29b-41d4-a716-446655440000"
    },
    {
      "event_type": "add_to_cart",
      "ref_type": "Product",
      "ref_id": "550e8400-e29b-41d4-a716-446655440000"
    }
  ]
}
```

| Field        | Type   | Validation                          | Description                             |
|--------------|--------|-------------------------------------|-----------------------------------------|
| `event_type` | string | required, min length 1              | One of the defined event type constants |
| `ref_type`   | string | required, must be valid enum value  | `Product`, `Category`, or `Brand`       |
| `ref_id`     | string | required                            | The ID of the referenced entity         |

**Response (200 OK):**

```json
{
  "message": "Interaction created successfully"
}
```

### GET `/api/v1/analytic/popularity/top`

Returns the top products ranked by popularity score in descending order, with pagination.

**Query parameters:**

| Parameter | Type | Default | Description                |
|-----------|------|---------|----------------------------|
| `page`    | int  | 1       | Page number (1-indexed)    |
| `limit`   | int  | 10      | Items per page (max 100)   |

**Response (200 OK):**

```json
[
  {
    "spu_id": "550e8400-e29b-41d4-a716-446655440000",
    "score": 42.5,
    "view_count": 100,
    "purchase_count": 15,
    "favorite_count": 8,
    "cart_count": 22,
    "review_count": 5,
    "date_updated": "2026-02-27T10:00:00.000Z"
  }
]
```

### GET `/api/v1/analytic/popularity/:spu_id`

Returns popularity data for a single product.

**Path parameters:**

| Parameter | Type | Description                   |
|-----------|------|-------------------------------|
| `spu_id`  | UUID | The product's SPU identifier  |

**Response (200 OK):**

```json
{
  "spu_id": "550e8400-e29b-41d4-a716-446655440000",
  "score": 42.5,
  "view_count": 100,
  "purchase_count": 15,
  "favorite_count": 8,
  "cart_count": 22,
  "review_count": 5,
  "date_updated": "2026-02-27T10:00:00.000Z"
}
```

---

## Interaction Tracking System

### Fire-and-Forget Pattern

The business layer provides two convenience methods that run interaction creation in a background goroutine so the calling code is never blocked:

```go
// Single interaction -- non-blocking
func (b *AnalyticBiz) TrackInteraction(account, eventType, refType, refID)

// Multiple interactions -- non-blocking
func (b *AnalyticBiz) TrackInteractions(interactions []CreateInteraction)
```

Both methods internally call `CreateInteraction`, which:

1. Maps the input to `CreateBatchInteractionParams` and calls the SQLC batch insert (`CreateBatchInteraction`).
2. For each successfully inserted row, publishes an `analyticmodel.Interaction` message to the Kafka topic `analytic.interaction`.
3. Errors are logged via `slog` but never propagated back to the caller -- this ensures analytics failures do not degrade user-facing request latency.
### Data Flow

```
Client HTTP Request
       |
       v
  Echo Handler (CreateInteraction)
       |
       v
  AnalyticBiz.CreateInteraction()
       |
       +---> PostgreSQL batch INSERT (analytic.interaction)
       |
       +---> Publish to Kafka topic "analytic.interaction"
                    |
                    v
             Kafka Consumer (InitPopularityPubsub)
                    |
                    v
             HandlePopularityEvent()
                    |
                    v
             PostgreSQL UPSERT (analytic.product_popularity)
```

---

## Popularity Scoring

### How It Works

Product popularity is computed as a running weighted sum. Every time an interaction event with `ref_type = "Product"` is consumed from the Kafka topic, the system:

1. Filters out non-product events (Category and Brand interactions are ignored).
2. Parses the `ref_id` as a UUID (the product's SPU ID).
3. Looks up the event's weight from `popularityWeights`.
4. Determines which counter to increment based on the event type.
5. Performs an atomic `UPSERT` that adds the weight to the cumulative `score` and increments the appropriate counter by 1.

### Event Weight Table

Weights are configurable via the `PopularityWeights` struct (YAML/mapstructure). Below are the default values:

| Event Type                | Weight  | Counter Incremented | Signal Type     |
|---------------------------|---------|---------------------|-----------------|
| `purchase`                | +0.80   | `purchase_count`    | High intent     |
| `add_to_favorites`        | +0.60   | `favorite_count`    | High intent     |
| `add_to_cart`             | +0.50   | `cart_count`        | High intent     |
| `write_review`            | +0.50   | `review_count`      | High intent     |
| `rating_high`             | +0.40   | `review_count`      | High intent     |
| `view`                    | +0.30   | `view_count`        | Discovery       |
| `ask_question`            | +0.25   | --                  | Additional      |
| `click_from_search`       | +0.20   | --                  | Discovery       |
| `click_from_recommendation` | +0.15 | --                  | Discovery       |
| `view_similar_products`   | +0.15   | --                  | Discovery       |
| `click_from_category`     | +0.12   | --                  | Discovery       |
| `rating_medium`           | +0.10   | `review_count`      | Neutral         |
| `view_bounce`             | -0.10   | `view_count`        | Negative        |
| `not_interested`          | -0.30   | --                  | Negative        |
| `remove_from_cart`        | -0.30   | `cart_count`        | Negative        |
| `hide_item`               | -0.35   | --                  | Negative        |
| `rating_low`              | -0.50   | `review_count`      | Negative        |
| `dislike`                 | -0.50   | --                  | Negative        |
| `return_product`          | -0.60   | --                  | Negative        |
| `cancel_order`            | -0.60   | --                  | Negative        |
| `refund_requested`        | -0.70   | --                  | Negative        |
| `report_product`          | -1.20   | --                  | Negative        |

Events not present in the weight map are silently skipped (no score impact, no counter increment).

### Score Accumulation

The `UpsertProductPopularity` query uses PostgreSQL's `ON CONFLICT ... DO UPDATE` to atomically add incoming deltas to existing values. This means:

- There is no read-before-write; the database handles the atomic increment.
- Negative weights (e.g., `report_product` at -1.20) decrease the score, so products with many negative signals will naturally fall in rank.
- The `score` column has a descending index, making "top N" queries efficient.

---

## Kafka Consumer Integration

### Topic

The module publishes and subscribes to a single Kafka topic:

```
analytic.interaction
```

This is defined as the constant `analyticmodel.TopicAnalyticInteraction`.

### Publisher

When `AnalyticBiz.CreateInteraction` successfully inserts an interaction row, it immediately publishes the full `Interaction` struct (JSON-encoded) to the topic. This happens synchronously within the batch result callback, one message per inserted row.

### Consumer

`InitPopularityPubsub()` registers a subscriber on the `analytic.interaction` topic. The subscriber:

1. Decodes the Kafka message payload into an `analyticmodel.Interaction` struct using `pubsub.DecodeWrap`.
2. Calls `HandlePopularityEvent`, which filters for product-scoped events and upserts the popularity record.
3. Logs errors but always returns `nil` to the Watermill subscriber, preventing message redelivery loops for permanently failing messages.

### pubsub.Client Abstraction

The module does not depend on Watermill or Kafka directly. Instead, it uses the internal `pubsub.Client` interface:

```go
type Client interface {
    Group(name string) Client
    Publish(topic string, value any) error
    Subscribe(topic string, handler func(msg *MessageDecoder) error) error
    Close() error
}
```

This abstraction allows swapping the underlying message broker (Kafka, NATS, in-memory, etc.) without changing the analytics module code.

---

## Config Structure

### PopularityWeights

Defined in `internal/module/analytic/config/config.go`. The struct supports YAML configuration via `mapstructure` tags.

```go
type PopularityWeights struct {
    Purchase            float64 `yaml:"purchase"`
    AddToCart           float64 `yaml:"add_to_cart"`
    View                float64 `yaml:"view"`
    AddToFavorites      float64 `yaml:"add_to_favorites"`
    WriteReview         float64 `yaml:"write_review"`
    RatingHigh          float64 `yaml:"rating_high"`
    RatingMedium        float64 `yaml:"rating_medium"`
    AskQuestion         float64 `yaml:"ask_question"`
    ClickFromSearch     float64 `yaml:"click_from_search"`
    ClickFromRecommend  float64 `yaml:"click_from_recommendation"`
    ClickFromCategory   float64 `yaml:"click_from_category"`
    ViewSimilarProducts float64 `yaml:"view_similar_products"`
    RemoveFromCart      float64 `yaml:"remove_from_cart"`
    ReturnProduct       float64 `yaml:"return_product"`
    RefundRequested     float64 `yaml:"refund_requested"`
    CancelOrder         float64 `yaml:"cancel_order"`
    RatingLow           float64 `yaml:"rating_low"`
    ReportProduct       float64 `yaml:"report_product"`
    Dislike             float64 `yaml:"dislike"`
    HideItem            float64 `yaml:"hide_item"`
    NotInterested       float64 `yaml:"not_interested"`
    ViewBounce          float64 `yaml:"view_bounce"`
}
```

The `WeightMap()` method converts the struct into a `map[string]float64` keyed by the event type string, which is what the business layer uses at runtime for O(1) weight lookups.

Default weights are provided by `DefaultPopularityWeights()` and are used when no external configuration overrides them.

---

## Models and Event Types

### Interaction (Domain Model)

Defined in `internal/module/analytic/model/analytic.go`. This is the struct published to and consumed from Kafka.

```go
type Interaction struct {
    ID            int64                                 `json:"id"`
    AccountID     uuid.NullUUID                         `json:"account_id"`
    AccountNumber int64                                 `json:"account_number"`
    EventType     string                                `json:"event_type"`
    RefType       analyticdb.AnalyticInteractionRefType `json:"ref_type"`
    RefID         string                                `json:"ref_id"`
    Metadata      json.RawMessage                       `json:"metadata"`
    DateCreated   time.Time                             `json:"date_created"`
}
```

### AnalyticInteractionRefType (Enum)

Maps to the PostgreSQL enum `analytic.interaction_ref_type`:

| Go Constant                            | Value        |
|----------------------------------------|--------------|
| `AnalyticInteractionRefTypeProduct`    | `"Product"`  |
| `AnalyticInteractionRefTypeCategory`   | `"Category"` |
| `AnalyticInteractionRefTypeBrand`      | `"Brand"`    |

### Event Type Constants

All event type strings are defined as constants in `analyticmodel`:

**High-Intent Events:**
- `purchase` -- user completed a purchase
- `add_to_cart` -- user added a product to their cart
- `view` -- user viewed a product page
- `write_review` -- user submitted a review
- `rating_high` -- user gave a high rating
- `rating_medium` -- user gave a medium rating
- `rating_low` -- user gave a low rating

**Discovery Events:**
- `click_from_search` -- user clicked a product from search results
- `click_from_recommendation` -- user clicked a recommended product
- `click_from_category` -- user clicked a product from category browsing
- `view_similar_products` -- user viewed the similar products section

**Additional Signals:**
- `add_to_favorites` -- user added a product to their favorites/wishlist
- `ask_question` -- user asked a question about a product

**Negative Signals:**
- `remove_from_cart` -- user removed a product from their cart
- `cancel_order` -- user cancelled an order
- `refund_requested` -- user requested a refund
- `return_product` -- user returned a product
- `report_product` -- user reported a product
- `dislike` -- user disliked a product
- `hide_item` -- user hid a product from their feed
- `not_interested` -- user marked a product as not interesting
- `view_bounce` -- user viewed a product but left very quickly

### AnalyticProductPopularity (Database Model)

```go
type AnalyticProductPopularity struct {
    SpuID         uuid.UUID `json:"spu_id"`
    Score         float64   `json:"score"`
    ViewCount     int64     `json:"view_count"`
    PurchaseCount int64     `json:"purchase_count"`
    FavoriteCount int64     `json:"favorite_count"`
    CartCount     int64     `json:"cart_count"`
    ReviewCount   int64     `json:"review_count"`
    DateUpdated   time.Time `json:"date_updated"`
}
```

---
## Key Patterns

### 1. SQLC Code Generation

All SQL queries are written in raw SQL files under `db/queries/` and compiled by SQLC v1.30.0 into type-safe Go code under `db/sqlc/`. The generated code uses `pgx/v5` as the database driver. This approach eliminates runtime query building and ensures SQL correctness at build time.

### 2. Flexible Filtering with `sqlc.slice` / `sqlc.narg`

The `ListInteraction`, `CountInteraction`, and `DeleteInteraction` queries use a pattern where every filter is optional:

```sql
("column" = ANY(sqlc.slice('column')) OR sqlc.slice('column') IS NULL)
```

When a slice parameter is `nil`, the condition is bypassed. This allows a single query to serve many different filter combinations without dynamic SQL generation.

### 3. Nullable Field Updates

The `UpdateInteraction` query uses a dual-parameter pattern for nullable columns:

```sql
"account_id" = CASE
    WHEN sqlc.arg('null_account_id')::bool = TRUE THEN NULL
    ELSE COALESCE(sqlc.narg('account_id'), "account_id")
END
```

This allows callers to explicitly set a field to `NULL` (by setting the `null_*` flag to `true`), leave it unchanged (by passing `nil` for both), or update it to a new value.

### 4. Event-Driven Decoupling

Interaction persistence and popularity scoring are decoupled through Kafka. The `CreateInteraction` method writes to the database and publishes synchronously, but the popularity computation happens asynchronously in a separate subscriber. This means:

- The HTTP response is not blocked by popularity computation.
- Popularity updates can be retried independently.
- Additional consumers can be added to the same topic without changing the producer.

### 5. Atomic Upsert Accumulation

Rather than reading the current popularity record, computing the new score in Go, and writing it back, the module pushes the delta into a single `INSERT ... ON CONFLICT DO UPDATE` statement. PostgreSQL handles the atomic increment, eliminating race conditions under concurrent writes.

### 6. Fire-and-Forget Goroutines

`TrackInteraction` and `TrackInteractions` launch goroutines that create interactions in the background. This is suitable for non-critical analytics where dropping an occasional event is acceptable in exchange for zero latency impact on the user-facing request path.

---
# Catalog Module

## Overview

The Catalog module is a core domain module of the ShopNexus e-commerce backend. It manages the product catalog including products (SPU/SKU), categories, brands, tags, comments/reviews, hybrid search (vector + BM25), and personalized recommendations. It is built with Go, PostgreSQL, SQLC for type-safe queries, Echo v4 for HTTP transport, and Uber fx for dependency injection.

The module follows a layered architecture:

- **Transport layer** (`transport/echo/`) -- HTTP handlers that parse requests, invoke business logic, and write responses.
- **Business logic layer** (`biz/`) -- Core domain logic, orchestration of cross-module calls, search synchronization, and recommendation feed management.
- **Data access layer** (`db/`) -- SQLC-generated queries and models operating against the `catalog` PostgreSQL schema.
- **Model layer** (`model/`) -- Domain DTOs shared across layers and modules.

---
## Database Schema

All tables reside in the `catalog` PostgreSQL schema.

### Enums

```sql
CREATE TYPE "catalog"."comment_ref_type" AS ENUM ('ProductSpu', 'Comment');
CREATE TYPE "catalog"."search_sync_ref_type" AS ENUM ('ProductSpu');
```

- `comment_ref_type` -- Determines whether a comment is attached to a product SPU or is a reply to another comment.
- `search_sync_ref_type` -- Tracks which entity types participate in search index synchronization (currently only `ProductSpu`).

### Tables

#### `catalog.brand`

Stores product brand information.

```sql
CREATE TABLE IF NOT EXISTS "catalog"."brand" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "code" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    CONSTRAINT "brand_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "brand_code_key" ON "catalog"."brand" ("code");
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `UUID` | Primary key, auto-generated |
| `code` | `TEXT` | Unique brand code/slug |
| `name` | `TEXT` | Display name |
| `description` | `TEXT` | Brand description |

#### `catalog.category`

Hierarchical product category tree.

```sql
CREATE TABLE IF NOT EXISTS "catalog"."category" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" VARCHAR(100) NOT NULL,
    "description" TEXT NOT NULL,
    "parent_id" BIGINT,
    CONSTRAINT "category_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "category_name_key" ON "catalog"."category" ("name");
CREATE INDEX IF NOT EXISTS "category_parent_id_idx" ON "catalog"."category" ("parent_id");
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `UUID` | Primary key, auto-generated |
| `name` | `VARCHAR(100)` | Unique category name |
| `description` | `TEXT` | Category description |
| `parent_id` | `BIGINT` | Self-referencing parent for tree hierarchy (nullable) |

#### `catalog.product_spu` (Standard Product Unit)

The SPU represents a product concept (e.g., "iPhone 15 Pro") independent of specific variants.

```sql
CREATE TABLE IF NOT EXISTS "catalog"."product_spu" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "slug" TEXT NOT NULL,
    "account_id" UUID NOT NULL,
    "category_id" UUID NOT NULL,
    "brand_id" UUID NOT NULL,
    "featured_sku_id" UUID,
    "name" TEXT NOT NULL,
    "description" TEXT NOT NULL,
    "is_active" BOOLEAN NOT NULL,
    "specifications" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_deleted" TIMESTAMPTZ(3),
    "number" BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    CONSTRAINT "product_spu_pkey" PRIMARY KEY ("id")
);
```

Key indexes:
- `product_spu_slug_key` -- Unique index on `slug`
- `product_spu_featured_sku_id_key` -- Unique index on `featured_sku_id`
- `product_spu_account_id_idx` -- Index on `account_id` (vendor)
- `product_spu_category_id_idx` -- Index on `category_id`
- `product_spu_brand_id_idx` -- Index on `brand_id`

Foreign keys:
- `category_id` -> `catalog.category(id)` ON DELETE CASCADE
- `brand_id` -> `catalog.brand(id)` ON DELETE CASCADE

| Column | Type | Description |
|--------|------|-------------|
| `id` | `UUID` | Primary key |
| `slug` | `TEXT` | URL-friendly unique identifier (auto-generated from name + UUID) |
| `account_id` | `UUID` | Vendor/seller account who owns this product |
| `category_id` | `UUID` | FK to category |
| `brand_id` | `UUID` | FK to brand |
| `featured_sku_id` | `UUID` | Optional FK to the "default" SKU variant shown on cards |
| `name` | `TEXT` | Product name |
| `description` | `TEXT` | Full product description |
| `is_active` | `BOOLEAN` | Whether the product is visible/purchasable |
| `specifications` | `JSONB` | Array of `{name, value}` specification pairs |
| `date_created` | `TIMESTAMPTZ(3)` | Creation timestamp |
| `date_updated` | `TIMESTAMPTZ(3)` | Last update timestamp |
| `date_deleted` | `TIMESTAMPTZ(3)` | Soft-delete timestamp (nullable) |
| `number` | `BIGINT` | Auto-incrementing sequence number (identity column) |

#### `catalog.product_sku` (Stock Keeping Unit)

Each SKU is a concrete purchasable variant of an SPU (e.g., "iPhone 15 Pro, 256GB, Blue").

```sql
CREATE TABLE IF NOT EXISTS "catalog"."product_sku" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "spu_id" UUID NOT NULL,
    "price" BIGINT NOT NULL,
    "can_combine" BOOLEAN NOT NULL,
    "attributes" JSONB NOT NULL,
    "package_details" JSONB NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_deleted" TIMESTAMPTZ(3),
    CONSTRAINT "product_sku_pkey" PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "product_sku_spu_id_idx" ON "catalog"."product_sku" ("spu_id");
```

Foreign keys:
- `spu_id` -> `catalog.product_spu(id)` ON DELETE CASCADE

| Column | Type | Description |
|--------|------|-------------|
| `id` | `UUID` | Primary key |
| `spu_id` | `UUID` | FK to parent SPU |
| `price` | `BIGINT` | Price in smallest currency unit (e.g., cents) |
| `can_combine` | `BOOLEAN` | Whether this SKU can be combined with promotions |
| `attributes` | `JSONB` | Array of `{name, value}` variant attributes |
| `package_details` | `JSONB` | Shipping/package metadata |
| `date_created` | `TIMESTAMPTZ(3)` | Creation timestamp |
| `date_deleted` | `TIMESTAMPTZ(3)` | Soft-delete timestamp (nullable) |

#### `catalog.tag`

Free-form tags for product classification and filtering.

```sql
CREATE TABLE IF NOT EXISTS "catalog"."tag" (
    "id" VARCHAR(100) NOT NULL,
    "description" VARCHAR(255),
    CONSTRAINT "tag_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `VARCHAR(100)` | The tag string itself (serves as PK) |
| `description` | `VARCHAR(255)` | Optional description of the tag |

#### `catalog.product_spu_tag`

Many-to-many junction table linking SPUs to tags.

```sql
CREATE TABLE IF NOT EXISTS "catalog"."product_spu_tag" (
    "id" BIGSERIAL NOT NULL,
    "spu_id" UUID NOT NULL,
    "tag" VARCHAR(100) NOT NULL,
    CONSTRAINT "product_spu_tag_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "product_spu_tag_spu_id_tag_key"
    ON "catalog"."product_spu_tag" ("spu_id", "tag");
```

Foreign keys:
- `spu_id` -> `catalog.product_spu(id)` ON DELETE CASCADE
- `tag` -> `catalog.tag(id)` ON DELETE CASCADE

#### `catalog.comment`

Reviews and nested comments on products.

```sql
CREATE TABLE IF NOT EXISTS "catalog"."comment" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "account_id" UUID NOT NULL,
    "ref_type" "catalog"."comment_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "body" TEXT NOT NULL,
    "upvote" BIGINT NOT NULL DEFAULT 0,
    "downvote" BIGINT NOT NULL DEFAULT 0,
    "score" DOUBLE PRECISION NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "comment_pkey" PRIMARY KEY ("id")
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `UUID` | Primary key |
| `account_id` | `UUID` | Author's account ID |
| `ref_type` | `comment_ref_type` | `'ProductSpu'` for product reviews, `'Comment'` for replies |
| `ref_id` | `UUID` | ID of the referenced entity (SPU or parent comment) |
| `body` | `TEXT` | Comment/review text |
| `upvote` | `BIGINT` | Upvote counter |
| `downvote` | `BIGINT` | Downvote counter |
| `score` | `DOUBLE PRECISION` | Rating score (0.0 to 1.0) |
| `date_created` | `TIMESTAMPTZ(3)` | Creation timestamp |
| `date_updated` | `TIMESTAMPTZ(3)` | Last update timestamp |

#### `catalog.search_sync`

Tracks which products need their search index re-synced (embeddings and/or metadata).

```sql
CREATE TABLE IF NOT EXISTS "catalog"."search_sync" (
    "id" BIGSERIAL NOT NULL,
    "ref_type" "catalog"."search_sync_ref_type" NOT NULL,
    "ref_id" UUID NOT NULL,
    "is_stale_embedding" BOOLEAN NOT NULL DEFAULT true,
    "is_stale_metadata" BOOLEAN NOT NULL DEFAULT true,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "search_sync_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "search_sync_ref_type_ref_id_key"
    ON "catalog"."search_sync" ("ref_type", "ref_id");
CREATE INDEX IF NOT EXISTS "search_sync_is_stale_embedding_idx"
    ON "catalog"."search_sync" ("is_stale_embedding");
CREATE INDEX IF NOT EXISTS "search_sync_is_stale_metadata_idx"
    ON "catalog"."search_sync" ("is_stale_metadata");
CREATE INDEX IF NOT EXISTS "search_sync_date_created_idx"
    ON "catalog"."search_sync" ("date_created");
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | `BIGSERIAL` | Primary key |
| `ref_type` | `search_sync_ref_type` | Entity type (currently only `'ProductSpu'`) |
| `ref_id` | `UUID` | ID of the entity to sync |
| `is_stale_embedding` | `BOOLEAN` | Whether the vector embedding needs regeneration |
| `is_stale_metadata` | `BOOLEAN` | Whether the metadata (name, price, etc.) needs re-sync |
| `date_created` | `TIMESTAMPTZ(3)` | When the sync record was created |
| `date_updated` | `TIMESTAMPTZ(3)` | When the sync record was last updated |

---

## API Endpoints

All endpoints are prefixed with `/api/v1/catalog`.

### Product Detail & Cards (Consumer-Facing)

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/product-detail` | Get full product detail by `id` or `slug` query param. Tracks view interaction if authenticated. | Optional |
| `GET` | `/product-card` | List product cards with pagination, optional `vendor_id` and `search` filters. Uses hybrid search when search query is provided. | No |
| `GET` | `/product-card/recommended` | Get personalized product recommendations for the authenticated user. Falls back to most-sold products. | Yes |

### Product SPU (Vendor/Admin)

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/product-spu` | List SPUs with pagination and filters (`code`, `category_id`, `brand_id`, `is_active`) | No |
| `GET` | `/product-spu/:id` | Get a single SPU by UUID | No |
| `POST` | `/product-spu` | Create a new SPU with category, brand, tags, resources, and specifications | Yes |
| `PATCH` | `/product-spu` | Update an existing SPU (partial update via JSON body) | Yes |
| `DELETE` | `/product-spu/:id` | Delete an SPU by UUID | Yes |

### Product SKU (Vendor/Admin)

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/product-sku` | List SKUs by `spu_id`, optional `price_from`, `price_to`, `can_combine` filters | No |
| `POST` | `/product-sku` | Create a new SKU under an SPU (also creates inventory stock record) | Yes |
| `PATCH` | `/product-sku` | Update SKU fields (price, attributes, package details) | Yes |
| `DELETE` | `/product-sku` | Delete a SKU by ID | Yes |

### Comments / Reviews

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/comment` | List comments with pagination. Requires `ref_type` and `ref_id`. Supports `score_from`/`score_to` filters. | Yes |
| `POST` | `/comment` | Create a comment/review with body, score, and optional resource attachments. Tracks analytic interactions. | Yes |
| `PATCH` | `/comment` | Update comment body, score, upvote/downvote deltas, and resources | Yes |
| `DELETE` | `/comment` | Delete comments by list of IDs (also removes associated resources) | Yes |

### Tags

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/tag` | List tags with pagination and optional `search` (ILIKE) | No |
| `GET` | `/tag/:tag` | Get a single tag by its string ID | Yes |

### Brands

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/brand` | List brands with pagination and optional `search` (ILIKE on name/description) | No |
| `GET` | `/brand/:id` | Get a single brand by UUID | No |

### Categories

| Method | Path | Description | Auth Required |
|--------|------|-------------|---------------|
| `GET` | `/category` | List categories with pagination and optional `search` (ILIKE on name/description) | No |
| `GET` | `/category/:id` | Get a single category by UUID | No |

---

## Business Logic

### CatalogBiz

The `CatalogBiz` struct in `biz/catalog.go` is the central orchestrator. It holds references to:

- `storage` -- PostgreSQL connection via `pgsqlc.Storage[*catalogdb.Queries]`, providing transactional query execution.
- `cache` -- Redis-compatible cache client for recommendation feeds and offsets.
- `pubsub` -- Pub/sub client for receiving interaction events from the analytics module.
- `common` -- Cross-cutting concerns: resource/image management.
- `account` -- User profile lookups for comment authors.
- `inventory` -- Stock management (creates stock records for new SKUs, queries sold counts).
- `promotion` -- Price calculation with active promotions applied.
- `analytic` -- Interaction tracking (views, reviews, ratings).
- `searchClient` -- HTTP client for communicating with the external search engine.

### Product SPU Lifecycle

1. **Create** -- Generates a slug from the product name appended with a UUID (`name-slug.uuid`). Creates the SPU row, associates tags (auto-creating any that do not yet exist), attaches resource files, and inserts a `search_sync` record marked as stale.
2. **Update** -- Partial update via `COALESCE`-based SQL. Re-syncs tags by deleting all existing associations and re-creating them. Marks the `search_sync` record as stale for metadata, and additionally stale for embedding if the description changed.
3. **Delete** -- Hard-deletes the SPU row. Foreign key cascades remove associated SKUs, tags, and related records.
4. **List** -- Fetches SPUs from the database, enriches each with its resolved category, brand, tags, rating aggregate, and resource URLs.

### Product SKU Lifecycle

1. **Create** -- Creates the SKU row and automatically provisions an inventory stock record (initial stock = 0) within the same transaction.
2. **Update** -- Partial update of price, attributes, and package details. Also marks the parent SPU's search sync as stale for metadata.
3. **Delete** -- Hard-deletes the SKU row.
4. **List** -- Fetches SKUs and joins with inventory stock data.

### Product Cards

Product cards are the compact representation used in listing pages and search results. `ProductCardsFromSpuIDs` is an internal method that:

1. Fetches SPUs by their IDs.
2. Resolves the featured SKU for each SPU to determine the display price.
3. Calculates promoted prices through the `promotion` module.
4. Aggregates ratings from comments.
5. Fetches resource images.
6. Maps active promotion codes.

### Product Detail

`GetProductDetail` assembles a complete product page view:

1. Fetches the SPU (by ID or slug).
2. Lists all SKUs for the SPU.
3. Resolves inventory stock and sold counts per SKU.
4. Calculates promoted prices for every SKU variant.
5. Fetches the detailed rating breakdown (5-star through 1-star counts).
6. Collects all unique promotion codes across SKUs.
7. Tracks a "view" interaction event if the requesting user is authenticated.

### Comments

- Comments use a polymorphic `ref_type`/`ref_id` pattern. A comment with `ref_type = 'ProductSpu'` is a product review; `ref_type = 'Comment'` is a reply to another comment.
- The `score` field (0.0 to 1.0) represents the review rating. Rating aggregations use percentage-based ranges: score >= 0.8 is "5 stars", 0.6-0.8 is "4 stars", etc.
- Upvotes and downvotes are updated via atomic delta increments (`upvote = upvote + delta`).
- Creating a review on a ProductSpu triggers analytic interactions: `EventWriteReview` plus a rating-tier event (`EventRatingHigh`, `EventRatingMed`, or `EventRatingLow`).
- Comment resources (images, attachments) are managed through the common module.

### Tag Management

Tags are lazily created. When a product SPU is created or updated with tags:

1. All existing tag associations for the SPU are deleted.
2. The system checks which of the requested tags already exist in `catalog.tag`.
3. Non-existing tags are bulk-inserted via `COPY FROM`.
4. New `product_spu_tag` associations are bulk-inserted.

---

## Search Architecture (Hybrid Vector + BM25)

### Overview

The catalog module integrates with an external search engine service that provides hybrid retrieval combining dense vector similarity (embedding-based) and sparse BM25 (keyword-based) scoring. The search engine is accessed via HTTP at a configurable URL.

### Configuration

Defined in the application config (`config.Search`):

```go
type Search struct {
    Url                          string        // Search engine base URL
    DenseWeight                  float32       // Weight for dense (vector) scoring component
    SparseWeight                 float32       // Weight for sparse (BM25) scoring component
    InteractionBatchSize         int           // Buffer size before flushing interaction events
    ProductMetadataSyncInterval  time.Duration // How often to sync product metadata
    ProductEmbeddingSyncInterval time.Duration // How often to sync product embeddings
}
```

### Search Client

The `SearchClient` in `biz/search.go` communicates with the external search service:

- **`POST /search`** -- Sends a query string with dense/sparse weights, offset, and limit. Returns a list of `ProductRecommend` (product IDs with relevance scores).
- **`POST /products`** -- Sends product details to be indexed. Supports a `metadata_only` flag to skip embedding regeneration when only non-textual fields changed.
- **`POST /events`** -- Sends user interaction events (views, purchases, reviews) for collaborative filtering.
- **`GET /recommend`** -- Retrieves personalized recommendations for a specific account.

### Search Request Flow

When a user searches via the `GET /product-card?search=...` endpoint:

1. The query is sent to the external search engine (`POST /search`) with configured dense/sparse weights.
2. The search engine returns ranked product IDs with scores.
3. These IDs are used to filter the SQL query (`SearchCountProductSpu`), preserving the search engine's ranking order.
4. If the search engine is unavailable, the system falls back to PostgreSQL `ILIKE` matching on `slug`, `name`, and `description`.
5. The results are enriched into `ProductCard` objects with prices, ratings, and images.

### Search Index Synchronization

The module maintains a `search_sync` table that tracks which products are out of date. Two types of staleness are tracked independently:

- **Metadata staleness** (`is_stale_metadata`) -- Triggered when any product field changes (name, price, tags, category, etc.). Requires re-indexing metadata in the search engine.
- **Embedding staleness** (`is_stale_embedding`) -- Triggered only when the product description changes. Requires regeneration of the dense vector embedding (computationally expensive).

#### Cron-Based Sync

Two independent goroutines run in the background:

1. **Metadata sync cron** -- Runs at `ProductMetadataSyncInterval`. Fetches up to 1000 stale records per batch, sends full product details to the search engine with `metadata_only = true`, and clears the stale flag.
2. **Embedding sync cron** -- Runs at `ProductEmbeddingSyncInterval`. Fetches up to 100 stale records per batch, sends product details for full re-indexing (including embedding regeneration), and clears both stale flags.

Stale records are fetched using `FOR UPDATE SKIP LOCKED` to allow concurrent cron workers without contention:

```sql
SELECT id, ref_id, ref_type
FROM catalog.search_sync
WHERE (is_stale_metadata = $1 OR is_stale_embedding = $2) AND ref_type = $3
ORDER BY date_updated ASC
FOR UPDATE SKIP LOCKED
LIMIT $4;
```

---

## Recommendation System

### Overview

The recommendation system combines collaborative filtering (from the external search/recommendation engine) with a "most sold" fallback to ensure users always see relevant products.

### Interaction Ingestion

User interactions (views, purchases, reviews) are published to the `analytic:interaction` pub/sub topic by the analytics module. The catalog module subscribes to this topic via `InitPubsub()`.

Interactions are buffered in-memory within the `SearchClient`:

1. Each event is appended to a buffer.
2. When the buffer reaches `InteractionBatchSize`, all buffered events are flushed to the search engine via `POST /events`.
3. After flushing, the user's cached recommendation feed is invalidated.

### Recommendation Feed

`ListRecommendedProductCard` implements a paginated recommendation feed using Redis sorted sets:

1. **Cache check** -- Attempts to read recommendations from the Redis sorted set `catalog:recommend:product:{account_id}`, respecting a feed offset stored at `catalog:recommend:offset:{account_id}`.
2. **Cache miss / exhaustion** -- When the feed is empty or the offset exceeds `CacheRecommendSize` (100), the system calls `GET /recommend?account_id=...&limit=100` to fetch fresh recommendations from the search engine.
3. **Cache population** -- Fresh recommendations are stored in a Redis sorted set, scored by relevance.
4. **Fallback** -- If recommendations do not fill the requested limit, the system supplements with randomly shuffled "most sold" products (queried from the inventory module).
5. **Enrichment** -- All recommended product IDs are resolved into full `ProductCard` objects through `ProductCardsFromSpuIDs`.

### Interleave Shuffle

The `InterleaveShuffle` utility function ensures diverse mixing of multiple recommendation sources. It splits each input slice into N proportional chunks, combines chunks pairwise, and shuffles within each chunk. This prevents any single source from dominating a contiguous section of the feed.

---

## Models and Types

### Domain Models

| Type | File | Description |
|------|------|-------------|
| `ProductSpu` | `model/product.go` | Full SPU with resolved category, brand, tags, rating, resources, specifications |
| `ProductSku` | `model/product.go` | SKU variant with price (currency), stock count, attributes, package details |
| `ProductCard` | `model/product_card.go` | Compact card representation for listing pages: price, original price, rating, promotions, resources |
| `ProductDetail` | `model/product_detail.go` | Full product detail page: all SKUs with promoted prices, rating breakdown, promotions, specifications |
| `ProductDetailSku` | `model/product_detail.go` | Per-SKU detail within a product page: price, original price, attributes, units taken/sold |
| `ProductAttribute` | `model/product_attribute.go` | Key-value pair for SKU variant attributes (`{name, value}`) |
| `ProductSpecification` | `model/product_attribute.go` | Key-value pair for SPU specifications (`{name, value}`) |
| `ProductRecommend` | `model/product_recommend.go` | Lightweight recommendation result: product UUID + relevance score |
| `Comment` | `model/comment.go` | Review/comment with author profile, body, upvote/downvote counts, score, timestamps, resources |
| `ProductRating` | `model/product_detail.go` | Aggregated rating: average score, total count, and star-level breakdown map |
| `Rating` | `model/catalog.go` | Simple score + total count pair (used in product cards) |
| `OrderPrice` | `model/catalog.go` | Calculated order pricing after promotions: product cost, ship cost, promotion codes |
| `RequestOrderPrice` | `model/catalog.go` | Input for price calculation: SKU/SPU IDs, unit price, quantity, ship cost |
| `ProductCardPromo` | `model/product_card.go` | Promotion badge for display on product cards |

### Database Models (SQLC-Generated)

| Type | Description |
|------|-------------|
| `CatalogBrand` | Brand row |
| `CatalogCategory` | Category row |
| `CatalogProductSpu` | Product SPU row |
| `CatalogProductSku` | Product SKU row |
| `CatalogTag` | Tag row |
| `CatalogProductSpuTag` | SPU-Tag junction row |
| `CatalogComment` | Comment row |
| `CatalogSearchSync` | Search sync tracking row |
| `CatalogCommentRefType` | Enum: `'ProductSpu'`, `'Comment'` |
| `CatalogSearchSyncRefType` | Enum: `'ProductSpu'` |

### Cache Keys

| Key Pattern | Type | Description |
|-------------|------|-------------|
| `catalog:recommend:product:{account_id}` | Redis Sorted Set | Cached recommendation feed per user |
| `catalog:recommend:offset:{account_id}` | Redis String (int64) | Current pagination offset into the recommendation feed |

---

## Key Patterns

### SPU/SKU (Standard Product Unit / Stock Keeping Unit)

The product model follows the industry-standard SPU/SKU pattern:
- **SPU** represents the abstract product concept with shared attributes (name, description, category, brand, specifications).
- **SKU** represents a concrete purchasable variant with variant-specific attributes (color, size), price, and inventory stock.

### Slug Generation

Product slugs are generated deterministically from the product name using the `gosimple/slug` library, appended with a UUID to guarantee uniqueness:

```go
func GenerateSlug(name string) string {
    return fmt.Sprintf("%s.%s", slug.Make(name), uuid.NewString())
}
```
### Search Fallback

The search system implements graceful degradation. If the external search engine is unavailable, the product card listing falls back to PostgreSQL `ILIKE` pattern matching:

```sql
("slug" ILIKE '%' || $query || '%') OR
("name" ILIKE '%' || $query || '%') OR
("description" ILIKE '%' || $query || '%')
```

### Interaction Buffering

To avoid overwhelming the search engine with individual interaction events, the system buffers events in memory and flushes them in configurable batch sizes. This trades off a small risk of data loss (in-memory buffer) for significantly reduced network overhead.

### Rating Score Ranges

Scores are stored as floating-point values between 0.0 and 1.0. For star-based display, the rating breakdown query maps score ranges:

| Score Range | Star Level |
|-------------|------------|
| `= 1.0 (100)` | 5 stars |
| `0.8 - 1.0 (80-100)` | 4 stars |
| `0.6 - 0.8 (60-80)` | 3 stars |
| `0.4 - 0.6 (40-60)` | 2 stars |
| `0.0 - 0.4 (0-40)` | 1 star |

Note: The SQL query uses integer percentages (0-100) while the Go models use decimal fractions (0.0-1.0).

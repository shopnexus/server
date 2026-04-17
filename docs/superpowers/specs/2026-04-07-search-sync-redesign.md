# Search Sync Redesign

Redesign the search sync system to support product, category, and tag synchronization to Milvus with clean file organization.

## Problem

The current sync system only handles products. Categories and tags have Milvus collections defined but nothing syncs data into them. The code is also tangled: `search.go` mixes query-side logic, sync processing, analytics events, and helpers in one 520-line file.

## Design Decisions

- **Embed category/tag from their own fields only** -- category: `name + ". " + description`, tag: `id + ". " + name + ". " + description`. No product-context enrichment.
- **Two cron loops (metadata + embedding), unified across entity types.** Metadata syncs fast (batch 1000, short interval). Embedding syncs slow (batch 32, longer interval). Both loops handle all three entity types (products, categories, tags) via type-switch dispatch.
- **Extend `search_sync` table** with `Category` and `Tag` ref_types. Same staleness tracking pattern as products.
- **File split by concern** -- query-side, sync, Milvus CRUD, analytics events.

## Database Changes

### Migration

```sql
ALTER TYPE "catalog"."search_sync_ref_type" ADD VALUE 'Category';
ALTER TYPE "catalog"."search_sync_ref_type" ADD VALUE 'Tag';
```

### Query: ListStaleSearchSync

Remove the `ref_type` filter. Both cron loops poll all entity types in one query:

```sql
SELECT id, ref_id, ref_type
FROM catalog.search_sync
WHERE (is_stale_metadata = @is_stale_metadata OR is_stale_embedding = @is_stale_embedding)
ORDER BY date_updated ASC
FOR UPDATE SKIP LOCKED
LIMIT @limit;
```

The caller passes `is_stale_metadata=true` (metadata loop) or `is_stale_embedding=true` (embedding loop). `FOR UPDATE SKIP LOCKED` prevents the two loops from grabbing the same rows.

### Seeder

The seeder creates `search_sync` rows for categories and tags, same as it already does for products. Both flags default to `true`.

## Sync Architecture

### Two Cron Loops

```
SetupCron()
  go startSyncCron(ctx, metadataInterval, metadataOnly=true)   // fast, batch 1000
  go startSyncCron(ctx, embeddingInterval, metadataOnly=false)  // slow, batch 32
```

Config keeps two intervals: `MetadataSyncInterval` and `EmbeddingSyncInterval`.

### Dispatch Flow

Each loop iteration:

1. `ListStaleSearchSync` (no ref_type filter, batch size depends on loop)
2. Group rows by `ref_type`
3. Type-switch dispatch:
   - `ProductSpu` -> `syncProducts(ctx, stales, metadataOnly)`
   - `Category` -> `syncCategories(ctx, stales, metadataOnly)`
   - `Tag` -> `syncTags(ctx, stales, metadataOnly)`

### Per-Entity Sync

**syncProducts(stales, metadataOnly):**
- Fetch `ProductDetail` via Restate ingress (same as today)
- If not metadataOnly: call LLM to embed `buildEmbeddingText(product)`
- Upsert to Milvus products collection (metadata-only reuses existing vectors)
- Clear the appropriate stale flag

**syncCategories(stales, metadataOnly):**
- If metadataOnly: skip entirely (categories collection has no scalar metadata fields, only id + vectors). Clear `is_stale_metadata` flag.
- Otherwise: fetch category rows from Postgres, call LLM to embed `name + ". " + description`, upsert to Milvus categories collection, clear `is_stale_embedding` flag.

**syncTags(stales, metadataOnly):**
- If metadataOnly: skip entirely (same reason as categories). Clear `is_stale_metadata` flag.
- Otherwise: fetch tag rows from Postgres, call LLM to embed `id + ". " + name + ". " + description`, upsert to Milvus tags collection, clear `is_stale_embedding` flag.

### Key Simplification: Drop Restate Send Indirection

The current flow: cron -> `restateclient.Send("Catalog", "UpdateProducts")` -> `UpdateProducts` method (re-checks staleness for Restate retry dedup).

New flow: cron -> `syncProducts()` directly. The retry-dedup re-check is unnecessary because `FOR UPDATE SKIP LOCKED` already prevents double processing. `UpdateProducts` is removed from the interface.

## Staleness Hooks

Where `search_sync` rows are created/updated:

| Entity | Create Hook | Update Hook |
|--------|-------------|-------------|
| ProductSpu | `CreateProductSpu` -> `CreateDefaultSearchSync(ProductSpu, id)` | `UpdateProductSpu` -> `UpdateStaleSearchSync(metadata=true, embedding=true if desc changed)` |
| Category | Seeder (for now) -> `CreateDefaultSearchSync(Category, id)` | Future admin CRUD follows same pattern |
| Tag | Seeder (for now) -> `CreateDefaultSearchSync(Tag, id)` | Future admin CRUD follows same pattern |

When admin CRUD for categories/tags is added to the biz layer, they hook into `search_sync` the same way products do.

## File Organization

### search.go -- Query Side

Query-time logic only. No sync, no analytics.

Contents:
- `SearchParams` struct
- `buildFilterExpr()` -- builds Milvus boolean expression from typed filter params
- `Search()` -- hybrid dense+sparse vector search with scalar filtering
- `GetRecommendations()` -- personalized recommendations from account interest vectors
- `mapToSparseEmbedding()` -- converts map to Milvus sparse embedding
- `toMilvusStringList()` -- formats string slice for Milvus filter expressions
- Shared helper types: `embeddingResult`

### search_sync.go -- Sync Cron + Dispatch

All sync-related logic: cron, polling, entity-specific sync functions, embedding text builders.

Contents:
- Constants: `EmbeddingSyncBatchSize = 32`, `MetadataSyncBatchSize = 1000`
- `SetupCron()` -- starts two goroutines
- `startSyncCron(ctx, interval, metadataOnly)` -- ticker loop with syncLock
- `syncStaleEntities(ctx, metadataOnly)` -- polls DB, groups by ref_type, dispatches
- `syncProducts(ctx, stales, metadataOnly)` -- product sync (absorbs current `updateStaleProducts` + `UpdateProducts`)
- `syncCategories(ctx, stales, metadataOnly)` -- category sync
- `syncTags(ctx, stales, metadataOnly)` -- tag sync
- `buildEmbeddingText(ProductDetail)` -- product embedding text builder
- `buildCategoryEmbeddingText(name, description)` -- category embedding text
- `buildTagEmbeddingText(id, name, description)` -- tag embedding text
- `appendUnique()` helper

### search_milvus.go -- Milvus CRUD

All Milvus read/write operations.

Contents:
- `upsertProducts()` -- upsert product metadata + vectors to products collection
- `upsertCategories()` -- upsert category vectors to categories collection
- `upsertTags()` -- upsert tag vectors to tags collection
- `getProductVectors()` -- fetch content_vector for product IDs
- `getProductAllVectors()` -- fetch content_vector + sparse_vector for product IDs
- `getAccountInterests()` -- fetch interest vectors for account IDs
- `upsertAccountInterests()` -- upsert account interest vectors
- `existingVectors` struct

### search_events.go -- Analytics Write Path

Interaction buffering and account interest processing.

Contents:
- `AddInteraction()` -- buffers events, flushes when batch full
- `ProcessEvents()` -- updates account interest vectors from interaction events
- `aggregateProductWeights()` -- sums event weights per product
- `accountOutputFields()` -- builds field list for account queries
- `accountInterests` struct
- `InterleaveShuffle()` -- interleave-shuffle utility

## Interface Changes

Remove from `CatalogBiz`:
- `UpdateProducts(ctx, UpdateProductsParams)` -- no longer called via Restate; sync is internal

No new public methods. Sync is entirely internal (cron-driven).

## What Does Not Change

- `milvus_collections.go` -- schemas and indexes already defined
- `Search()` and `GetRecommendations()` query logic -- just moves to cleaned `search.go`
- `ProcessEvents()` and `AddInteraction()` logic -- just moves to `search_events.go`
- Product staleness hooks in `product_spu.go`
- Config field names `MetadataSyncInterval` and `EmbeddingSyncInterval` (renamed from `Product*` prefix)

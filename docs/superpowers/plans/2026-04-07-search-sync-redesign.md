# Search Sync Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the search sync system to support product, category, and tag synchronization to Milvus with clean file organization.

**Architecture:** Two cron loops (metadata-fast, embedding-slow) poll a unified `search_sync` table for all entity types. A type-switch dispatcher routes stale rows to entity-specific sync functions. Files are reorganized by concern: query-side, sync, Milvus CRUD, analytics events.

**Tech Stack:** Go, PostgreSQL (SQLC), Milvus, Restate

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/module/catalog/db/migrations/0002_search_sync_types.up.sql` | Create | Add Category/Tag to search_sync_ref_type enum |
| `internal/module/catalog/db/migrations/0002_search_sync_types.down.sql` | Create | Rollback migration |
| `internal/module/catalog/db/queries/system.sql` | Modify | Remove ref_type filter from ListStaleSearchSync |
| `config/struct.go` | Modify | Rename config fields |
| `config/config.default.yml` | Modify | Rename config keys |
| `config/config.dev.yml` | Modify | Rename config keys (if exists) |
| `internal/module/catalog/biz/search.go` | Rewrite | Query-side only |
| `internal/module/catalog/biz/search_sync.go` | Rewrite | Unified cron + dispatch + entity sync |
| `internal/module/catalog/biz/search_milvus.go` | Modify | Add upsertCategories, upsertTags |
| `internal/module/catalog/biz/search_events.go` | Create | Analytics write path (moved from search.go) |
| `internal/module/catalog/biz/interface.go` | Modify | Remove UpdateProducts from interface |
| `cmd/seed/helpers.go` | Modify | Add search_sync rows for categories/tags |

---

### Task 1: Database Migration — Extend search_sync_ref_type Enum

**Files:**
- Create: `internal/module/catalog/db/migrations/0002_search_sync_types.up.sql`
- Create: `internal/module/catalog/db/migrations/0002_search_sync_types.down.sql`

- [ ] **Step 1: Create up migration**

```sql
-- 0002_search_sync_types.up.sql
ALTER TYPE "catalog"."search_sync_ref_type" ADD VALUE IF NOT EXISTS 'Category';
ALTER TYPE "catalog"."search_sync_ref_type" ADD VALUE IF NOT EXISTS 'Tag';
```

- [ ] **Step 2: Create down migration**

```sql
-- 0002_search_sync_types.down.sql
-- PostgreSQL does not support removing enum values.
-- To roll back, recreate the type and migrate data.
-- This is intentionally left as a no-op for safety.
```

- [ ] **Step 3: Run migration**

```bash
make migrate
```

- [ ] **Step 4: Verify enum values exist**

```bash
psql -U shopnexus -d shopnexus -c "SELECT enum_range(NULL::catalog.search_sync_ref_type);"
```

Expected: `{ProductSpu,Category,Tag}`

- [ ] **Step 5: Commit**

```bash
git add internal/module/catalog/db/migrations/0002_search_sync_types.*
git commit -m "add Category and Tag to search_sync_ref_type enum"
```

---

### Task 2: Update SQLC Query — Remove ref_type Filter from ListStaleSearchSync

**Files:**
- Modify: `internal/module/catalog/db/queries/system.sql`

- [ ] **Step 1: Update ListStaleSearchSync query**

In `internal/module/catalog/db/queries/system.sql`, replace the existing `ListStaleSearchSync` query:

```sql
-- name: ListStaleSearchSync :many
SELECT id, ref_id, ref_type
FROM catalog.search_sync
WHERE (is_stale_metadata = sqlc.narg('is_stale_metadata') OR is_stale_embedding = sqlc.narg('is_stale_embedding'))
ORDER BY date_updated ASC
FOR UPDATE SKIP LOCKED
LIMIT sqlc.arg('limit');
```

The change: remove `AND ref_type = $1` — the cron now polls all entity types in one query.

- [ ] **Step 2: Regenerate SQLC**

```bash
sqlc generate
```

- [ ] **Step 3: Verify the generated ListStaleSearchSyncParams no longer has RefType field**

Check `internal/module/catalog/db/sqlc/system.sql.go` — the `ListStaleSearchSyncParams` struct should only have `IsStaleMetadata`, `IsStaleEmbedding`, and `Limit` fields.

- [ ] **Step 4: Commit**

```bash
git add internal/module/catalog/db/queries/system.sql internal/module/catalog/db/sqlc/
git commit -m "remove ref_type filter from ListStaleSearchSync query"
```

---

### Task 3: Rename Config Fields

**Files:**
- Modify: `config/struct.go`
- Modify: `config/config.default.yml`
- Modify: `config/config.dev.yml` (if it exists with search config)

- [ ] **Step 1: Update config struct**

In `config/struct.go`, rename the Search struct fields:

```go
type Search struct {
	DenseWeight          float32       `yaml:"denseWeight" mapstructure:"denseWeight" validate:"required,gte=0,lte=1"`
	SparseWeight         float32       `yaml:"sparseWeight" mapstructure:"sparseWeight" validate:"required,gte=0,lte=1"`
	InteractionBatchSize int           `yaml:"interactionBatchSize" mapstructure:"interactionBatchSize" validate:"required,gte=1"`
	MetadataSyncInterval  time.Duration `yaml:"metadataSyncInterval" mapstructure:"metadataSyncInterval" validate:"gte=0"`
	EmbeddingSyncInterval time.Duration `yaml:"embeddingSyncInterval" mapstructure:"embeddingSyncInterval" validate:"gte=0"`
}
```

Remove the `Product` prefix from field names, yaml tags, and mapstructure tags. Remove the doc comments (the field names are self-explanatory now that they're not product-specific).

- [ ] **Step 2: Update config YAML files**

In `config/config.default.yml`, rename under `search:`:

```yaml
  search:
    denseWeight: 0.75
    sparseWeight: 0.25
    interactionBatchSize: 1
    metadataSyncInterval: 1s
    embeddingSyncInterval: 1s
```

Do the same in `config/config.dev.yml` if it overrides these values.

- [ ] **Step 3: Update all Go references**

Search for `ProductMetadataSyncInterval` and `ProductEmbeddingSyncInterval` across the codebase and rename to `MetadataSyncInterval` and `EmbeddingSyncInterval`. The only reference should be in `search_sync.go` (which will be rewritten in Task 6), but check for others:

```bash
grep -r "ProductMetadata\|ProductEmbedding" --include="*.go" .
```

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add config/struct.go config/config.default.yml config/config.dev.yml
git commit -m "rename sync config fields, drop Product prefix"
```

---

### Task 4: Create search_events.go — Move Analytics Code

**Files:**
- Create: `internal/module/catalog/biz/search_events.go`
- Modify: `internal/module/catalog/biz/search.go` (remove moved code)

- [ ] **Step 1: Create search_events.go**

Create `internal/module/catalog/biz/search_events.go` with the following code moved from `search.go`:

```go
package catalogbiz

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"
	"github.com/milvus-io/milvus/client/v2/milvusclient"

	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	catalogutil "shopnexus-server/internal/module/catalog/util"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// AddInteraction buffers an analytic interaction event and flushes the batch when full.
func (b *CatalogHandler) AddInteraction(ctx restate.Context, params analyticmodel.Interaction) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buffer = append(b.buffer, params)

	if len(b.buffer) >= b.batchSize {
		toInsert := b.buffer
		b.buffer = make([]analyticmodel.Interaction, 0, b.batchSize)

		if err := b.ProcessEvents(ctx, toInsert); err != nil {
			return err
		}

		seen := make(map[uuid.UUID]struct{})
		for _, ev := range toInsert {
			if !ev.AccountID.Valid {
				continue
			}
			if _, ok := seen[ev.AccountID.UUID]; ok {
				continue
			}
			seen[ev.AccountID.UUID] = struct{}{}
			if err := b.cache.Delete(ctx, fmt.Sprintf(catalogmodel.CacheKeyRecommendProduct, ev.AccountID.UUID.String())); err != nil {
				slog.Error("failed to reset feed offset for account", slog.String("account_id", ev.AccountID.UUID.String()), slog.Any("error", err))
			}
		}
	}
	return nil
}

// ProcessEvents updates account interest vectors in Milvus based on analytic interaction events.
func (b *CatalogHandler) ProcessEvents(ctx restate.Context, events []analyticmodel.Interaction) error {
	if len(events) == 0 {
		return nil
	}

	itemIDSet := make(map[string]struct{})
	for _, e := range events {
		if e.RefID != "" {
			itemIDSet[e.RefID] = struct{}{}
		}
	}
	itemIDs := make([]string, 0, len(itemIDSet))
	for id := range itemIDSet {
		itemIDs = append(itemIDs, id)
	}

	itemVectors, err := b.getProductVectors(ctx, itemIDs)
	if err != nil {
		return sharedmodel.WrapErr("get product vectors", err)
	}

	accountEvents := make(map[string][]analyticmodel.Interaction)
	for _, e := range events {
		if !e.AccountID.Valid {
			continue
		}
		aid := e.AccountID.UUID.String()
		accountEvents[aid] = append(accountEvents[aid], e)
	}

	accountIDs := make([]string, 0, len(accountEvents))
	for id := range accountEvents {
		accountIDs = append(accountIDs, id)
	}
	existingAccounts, err := b.getAccountInterests(ctx, accountIDs)
	if err != nil {
		return sharedmodel.WrapErr("get account interests", err)
	}

	for accountID, acctEvents := range accountEvents {
		interests, strengths := catalogutil.DefaultInterests(ContentVectorDim)
		if existing, ok := existingAccounts[accountID]; ok {
			interests = existing.interests
			strengths = existing.strengths
		}

		productWeights := aggregateProductWeights(acctEvents)

		for productID, weight := range productWeights {
			productVec, ok := itemVectors[productID]
			if !ok {
				continue
			}
			if weight > 0 {
				catalogutil.AssignPositive(interests, strengths, productVec, weight)
			} else if weight < 0 {
				catalogutil.AssignNegative(interests, strengths, productVec, weight)
			}
		}

		if err := b.upsertAccountInterests(ctx, accountID, acctEvents[0].AccountNumber, interests, strengths); err != nil {
			return sharedmodel.WrapErr(fmt.Sprintf("upsert account %s", accountID), err)
		}
	}

	return nil
}

type accountInterests struct {
	interests [][]float32
	strengths []float32
}

func aggregateProductWeights(events []analyticmodel.Interaction) map[string]float32 {
	weights := make(map[string]float32)
	for _, e := range events {
		if e.RefID == "" {
			continue
		}
		weights[e.RefID] += catalogutil.GetEventWeight(strings.ToLower(e.EventType))
	}
	return weights
}

func accountOutputFields() []string {
	fields := make([]string, 0, 1+catalogutil.NumInterests*2)
	fields = append(fields, "id")
	for i := 1; i <= catalogutil.NumInterests; i++ {
		fields = append(fields, fmt.Sprintf("interest_%d", i))
		fields = append(fields, fmt.Sprintf("strength_%d", i))
	}
	return fields
}

// InterleaveShuffle splits each input slice into numParts chunks,
// combines the chunks for each part, and shuffles within each part.
func InterleaveShuffle[T any](numParts int, groups ...[]T) []T {
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	if total == 0 || numParts <= 0 {
		return nil
	}
	if numParts > total {
		numParts = total
	}

	splitInto := func(items []T) [][]T {
		parts := make([][]T, numParts)
		partSize := len(items) / numParts
		remainder := len(items) % numParts
		idx := 0
		for i := 0; i < numParts; i++ {
			size := partSize
			if i < remainder {
				size++
			}
			parts[i] = items[idx : idx+size]
			idx += size
		}
		return parts
	}

	splits := make([][][]T, len(groups))
	for i, g := range groups {
		splits[i] = splitInto(g)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	result := make([]T, 0, total)

	for i := 0; i < numParts; i++ {
		var part []T
		for _, s := range splits {
			part = append(part, s[i]...)
		}

		rng.Shuffle(len(part), func(a, b int) {
			part[a], part[b] = part[b], part[a]
		})

		result = append(result, part...)
	}

	return result
}
```

- [ ] **Step 2: Remove moved code from search.go**

Remove from `internal/module/catalog/biz/search.go`:
- `AddInteraction` method (lines 510-544)
- `ProcessEvents` method (lines 245-318)
- `accountInterests` struct (lines 413-416)
- `aggregateProductWeights` function (lines 418-427)
- `accountOutputFields` function (lines 429-437)
- `InterleaveShuffle` function (lines 549-599)

Also remove now-unused imports: `"math/rand"`, `analyticmodel`, `catalogutil` (if no longer used in search.go).

- [ ] **Step 3: Verify build**

```bash
go build ./internal/module/catalog/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/module/catalog/biz/search_events.go internal/module/catalog/biz/search.go
git commit -m "extract analytics code to search_events.go"
```

---

### Task 5: Rewrite search.go — Query Side Only

**Files:**
- Rewrite: `internal/module/catalog/biz/search.go`

After Task 4 removed the analytics code, this task cleans search.go down to query-side only. Remove the remaining sync-related code (`UpdateProducts`, `UpdateProductsParams`, `buildEmbeddingText`, `appendUnique`, `embeddingResult`) that will move to search_sync.go in Task 6.

- [ ] **Step 1: Rewrite search.go**

`internal/module/catalog/biz/search.go` should contain only:

```go
package catalogbiz

import (
	"fmt"
	"strings"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"

	"shopnexus-server/internal/infras/milvus"
	accountmodel "shopnexus-server/internal/module/account/model"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	catalogutil "shopnexus-server/internal/module/catalog/util"
	"github.com/guregu/null/v6"
	sharedmodel "shopnexus-server/internal/shared/model"
)

type SearchParams struct {
	sharedmodel.PaginationParams
	Collection string
	Query      string

	// Scalar filters (applied inside Milvus before ANN ranking)
	AccountID       []uuid.UUID
	CategoryID      []uuid.UUID
	Tags            []string
	IsActive        null.Bool
	PriceMin        null.Float
	PriceMax        null.Float
	DateCreatedFrom null.Int
	DateCreatedTo   null.Int
}

func buildFilterExpr(params SearchParams) string {
	var clauses []string

	if len(params.AccountID) > 0 {
		ids := make([]string, len(params.AccountID))
		for i, id := range params.AccountID {
			ids[i] = id.String()
		}
		clauses = append(clauses, fmt.Sprintf("account_id in %s", toMilvusStringList(ids)))
	}
	if len(params.CategoryID) > 0 {
		ids := make([]string, len(params.CategoryID))
		for i, id := range params.CategoryID {
			ids[i] = id.String()
		}
		clauses = append(clauses, fmt.Sprintf("category_id in %s", toMilvusStringList(ids)))
	}
	if len(params.Tags) > 0 {
		clauses = append(clauses, fmt.Sprintf("array_contains_any(tags, %s)", toMilvusStringList(params.Tags)))
	}
	if params.IsActive.Valid {
		clauses = append(clauses, fmt.Sprintf("is_active == %t", params.IsActive.Bool))
	}
	if params.PriceMin.Valid {
		clauses = append(clauses, fmt.Sprintf("price_min >= %f", params.PriceMin.Float64))
	}
	if params.PriceMax.Valid {
		clauses = append(clauses, fmt.Sprintf("price_max <= %f", params.PriceMax.Float64))
	}
	if params.DateCreatedFrom.Valid {
		clauses = append(clauses, fmt.Sprintf("date_created >= %d", params.DateCreatedFrom.Int64))
	}
	if params.DateCreatedTo.Valid {
		clauses = append(clauses, fmt.Sprintf("date_created <= %d", params.DateCreatedTo.Int64))
	}

	return strings.Join(clauses, " && ")
}

func (b *CatalogHandler) Search(ctx restate.Context, params SearchParams) ([]catalogmodel.ProductRecommend, error) {
	embeddings, err := b.llm.Embed(ctx, []string{params.Query})
	if err != nil {
		return nil, sharedmodel.WrapErr("embed query", err)
	}
	if len(embeddings) == 0 {
		return nil, catalogmodel.ErrNoEmbeddingsResult.Terminal()
	}
	emb := embeddings[0]

	pag := params.Constrain()
	limit := int(pag.Limit.Int32)
	offset := 0
	if pag.Offset().Valid {
		offset = int(pag.Offset().Int32)
	}

	filter := buildFilterExpr(params)

	denseReq := milvusclient.NewAnnRequest("content_vector", limit, entity.FloatVector(emb.Dense)).
		WithOffset(offset)
	if filter != "" {
		denseReq.WithFilter(filter)
	}

	var results []milvus.SearchResult

	if emb.Sparse != nil {
		sparseVec := mapToSparseEmbedding(emb.Sparse)
		sparseReq := milvusclient.NewAnnRequest("sparse_vector", limit, sparseVec).
			WithOffset(offset)
		if filter != "" {
			sparseReq.WithFilter(filter)
		}

		reranker := milvusclient.NewWeightedReranker([]float64{
			float64(b.denseWeight),
			float64(b.sparseWeight),
		})

		results, err = b.milvus.HybridSearch(ctx, CollectionProducts,
			limit, reranker, []string{"id"},
			denseReq, sparseReq,
		)
	} else {
		results, err = b.milvus.Search(ctx, CollectionProducts,
			limit, []entity.Vector{entity.FloatVector(emb.Dense)},
			"content_vector", []string{"id"},
		)
	}
	if err != nil {
		return nil, sharedmodel.WrapErr("search", err)
	}

	products := make([]catalogmodel.ProductRecommend, 0, len(results))
	for _, r := range results {
		id, err := uuid.Parse(r.ID)
		if err != nil {
			continue
		}
		products = append(products, catalogmodel.ProductRecommend{ID: id, Score: r.Score})
	}
	return products, nil
}

type GetRecommendationsParams struct {
	Account accountmodel.AuthenticatedAccount
	Limit   int32
}

func (b *CatalogHandler) GetRecommendations(ctx restate.Context, params GetRecommendationsParams) ([]catalogmodel.ProductRecommend, error) {
	accountID := params.Account.ID.String()

	rs, err := b.milvus.Query(ctx, CollectionAccounts,
		fmt.Sprintf("id == '%s'", accountID),
		accountOutputFields(),
	)
	if err != nil {
		return nil, sharedmodel.WrapErr("query account", err)
	}
	if rs.ResultCount == 0 {
		return nil, nil
	}

	var searchReqs []*milvusclient.AnnRequest
	var weights []float64

	for i := 1; i <= catalogutil.NumInterests; i++ {
		strengthCol := rs.GetColumn(fmt.Sprintf("strength_%d", i))
		if strengthCol == nil {
			continue
		}
		strengthF64, err := strengthCol.GetAsDouble(0)
		if err != nil || strengthF64 <= 0 {
			continue
		}

		interestCol := rs.GetColumn(fmt.Sprintf("interest_%d", i))
		if interestCol == nil {
			continue
		}
		vecAny, err := interestCol.Get(0)
		if err != nil {
			continue
		}
		vec, ok := vecAny.(entity.FloatVector)
		if !ok {
			continue
		}

		req := milvusclient.NewAnnRequest("content_vector", int(params.Limit), vec).
			WithFilter("is_active == true")
		searchReqs = append(searchReqs, req)
		weights = append(weights, strengthF64)
	}

	if len(searchReqs) == 0 {
		return nil, nil
	}

	maxW := weights[0]
	for _, w := range weights[1:] {
		if w > maxW {
			maxW = w
		}
	}
	if maxW > 0 {
		for i := range weights {
			weights[i] /= maxW
		}
	}

	reranker := milvusclient.NewWeightedReranker(weights)
	results, err := b.milvus.HybridSearch(ctx, CollectionProducts,
		int(params.Limit), reranker, []string{"id"},
		searchReqs...,
	)
	if err != nil {
		return nil, sharedmodel.WrapErr("recommend search", err)
	}

	products := make([]catalogmodel.ProductRecommend, 0, len(results))
	for _, r := range results {
		id, err := uuid.Parse(r.ID)
		if err != nil {
			continue
		}
		products = append(products, catalogmodel.ProductRecommend{ID: id, Score: r.Score})
	}
	return products, nil
}

func mapToSparseEmbedding(m map[uint32]float32) entity.SparseEmbedding {
	positions := make([]uint32, 0, len(m))
	values := make([]float32, 0, len(m))
	for k, v := range m {
		positions = append(positions, k)
		values = append(values, v)
	}
	emb, _ := entity.NewSliceSparseEmbedding(positions, values)
	return emb
}

// toMilvusStringList formats a string slice as ['a','b'] for Milvus filter expressions.
func toMilvusStringList(ids []string) string {
	if len(ids) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('\'')
		b.WriteString(id)
		b.WriteByte('\'')
	}
	b.WriteByte(']')
	return b.String()
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/module/catalog/...
```

This will fail because `UpdateProducts` and `embeddingResult` are referenced in search_sync.go still. That's expected — Task 6 rewrites search_sync.go.

- [ ] **Step 3: Commit (with Task 6)**

Hold commit until Task 6 completes so the build stays green.

---

### Task 6: Rewrite search_sync.go — Unified Cron + All Entity Sync

**Files:**
- Rewrite: `internal/module/catalog/biz/search_sync.go`

- [ ] **Step 1: Rewrite search_sync.go**

Replace the entire file with:

```go
package catalogbiz

import (
	"context"
	"log"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"

	restateclient "shopnexus-server/internal/infras/restate"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	"shopnexus-server/internal/shared/htmlutil"
	sharedmodel "shopnexus-server/internal/shared/model"
)

const (
	MetadataSyncBatchSize  = 1000
	EmbeddingSyncBatchSize = 32
)

// SetupCron starts two background cron loops:
// - metadata loop (fast, large batch) — syncs scalar fields to Milvus
// - embedding loop (slow, small batch) — regenerates vectors via LLM
func (b *CatalogHandler) SetupCron() error {
	metadataInterval := b.config.App.Search.MetadataSyncInterval
	if metadataInterval <= 0 {
		metadataInterval = time.Second
	}

	embeddingInterval := b.config.App.Search.EmbeddingSyncInterval
	if embeddingInterval <= 0 {
		embeddingInterval = time.Second
	}

	go b.startSyncCron(context.Background(), metadataInterval, true)
	go b.startSyncCron(context.Background(), embeddingInterval, false)
	return nil
}

func (b *CatalogHandler) startSyncCron(ctx context.Context, interval time.Duration, metadataOnly bool) {
	label := "metadata"
	if !metadataOnly {
		label = "embedding"
	}
	log.Printf("Starting %s sync cron...", label)

	if err := b.syncStaleEntities(ctx, metadataOnly); err != nil {
		log.Printf("Initial %s sync failed: %v", label, err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			log.Printf("Stopping %s sync cron...", label)
			return
		}

		b.syncLock.Lock()
		if err := b.syncStaleEntities(ctx, metadataOnly); err != nil {
			log.Printf("%s sync failed: %v", label, err)
		}
		b.syncLock.Unlock()
	}
}

// syncStaleEntities polls for stale rows across all entity types and dispatches to per-type handlers.
func (b *CatalogHandler) syncStaleEntities(ctx context.Context, metadataOnly bool) error {
	batchSize := MetadataSyncBatchSize
	if !metadataOnly {
		batchSize = EmbeddingSyncBatchSize
	}

	stales, err := b.storage.Querier().ListStaleSearchSync(ctx, catalogdb.ListStaleSearchSyncParams{
		Limit:            int32(batchSize),
		IsStaleMetadata:  null.BoolFrom(metadataOnly),
		IsStaleEmbedding: null.BoolFrom(!metadataOnly),
	})
	if err != nil {
		return sharedmodel.WrapErr("list stale search sync", err)
	}
	if len(stales) == 0 {
		return nil
	}

	// Group by ref_type
	groups := make(map[catalogdb.CatalogSearchSyncRefType][]catalogdb.ListStaleSearchSyncRow)
	for _, s := range stales {
		groups[s.RefType] = append(groups[s.RefType], s)
	}

	for refType, rows := range groups {
		var syncErr error
		switch refType {
		case catalogdb.CatalogSearchSyncRefTypeProductSpu:
			syncErr = b.syncProducts(ctx, rows, metadataOnly)
		case catalogdb.CatalogSearchSyncRefTypeCategory:
			syncErr = b.syncCategories(ctx, rows, metadataOnly)
		case catalogdb.CatalogSearchSyncRefTypeTag:
			syncErr = b.syncTags(ctx, rows, metadataOnly)
		default:
			slog.Warn("unknown search_sync ref_type", "ref_type", refType)
			continue
		}
		if syncErr != nil {
			slog.Error("sync failed", "ref_type", refType, "error", syncErr)
		}
	}

	return nil
}

// --- Product sync ---

func (b *CatalogHandler) syncProducts(ctx context.Context, stales []catalogdb.ListStaleSearchSyncRow, metadataOnly bool) error {
	log.Printf("Syncing %d stale products (metadataOnly=%v)...", len(stales), metadataOnly)

	// Fetch product details via Restate ingress
	var products []catalogmodel.ProductDetail
	for _, stale := range stales {
		detail, err := restateclient.Call[catalogmodel.ProductDetail](ctx, b.restateClient, "Catalog", "GetProductDetail", GetProductDetailParams{
			ID: uuid.NullUUID{UUID: stale.RefID, Valid: true},
		})
		if err != nil {
			slog.Error("get product detail for sync", "product_id", stale.RefID, "error", err)
			continue
		}
		products = append(products, detail)
	}

	if len(products) == 0 {
		return nil
	}

	// Generate embeddings if needed
	var embeddingMap map[string]embeddingResult
	if !metadataOnly {
		texts := make([]string, len(products))
		for i, p := range products {
			texts[i] = buildEmbeddingText(p)
		}
		embeddings, err := b.llm.Embed(ctx, texts)
		if err != nil {
			return sharedmodel.WrapErr("embed products", err)
		}
		embeddingMap = make(map[string]embeddingResult, len(products))
		for i, p := range products {
			embeddingMap[p.ID.String()] = embeddingResult{
				dense:  embeddings[i].Dense,
				sparse: embeddings[i].Sparse,
			}
		}
	}

	// Upsert to Milvus
	if err := b.upsertProducts(ctx, products, embeddingMap, metadataOnly); err != nil {
		return sharedmodel.WrapErr("upsert products", err)
	}

	// Clear stale flags
	return clearStaleFlagsProducts(b, ctx, products, metadataOnly)
}

// --- Category sync ---

func (b *CatalogHandler) syncCategories(ctx context.Context, stales []catalogdb.ListStaleSearchSyncRow, metadataOnly bool) error {
	// Categories collection has no scalar metadata — only id + vectors.
	// Metadata loop just clears the flag.
	if metadataOnly {
		return b.clearStaleFlagsByRows(ctx, stales, true)
	}

	log.Printf("Syncing %d stale category embeddings...", len(stales))

	ids := make([]uuid.UUID, len(stales))
	for i, s := range stales {
		ids[i] = s.RefID
	}

	categories, err := b.storage.Querier().ListCategory(ctx, catalogdb.ListCategoryParams{
		ID:    ids,
		Limit: null.Int32From(int32(len(ids))),
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch categories", err)
	}
	if len(categories) == 0 {
		return nil
	}

	// Build embedding texts
	texts := make([]string, len(categories))
	for i, c := range categories {
		texts[i] = buildCategoryEmbeddingText(c.Name, c.Description)
	}

	embeddings, err := b.llm.Embed(ctx, texts)
	if err != nil {
		return sharedmodel.WrapErr("embed categories", err)
	}

	// Upsert to Milvus
	if err := b.upsertCategories(ctx, categories, embeddings); err != nil {
		return sharedmodel.WrapErr("upsert categories", err)
	}

	return clearStaleFlagsCategories(b, ctx, categories, false)
}

// --- Tag sync ---

func (b *CatalogHandler) syncTags(ctx context.Context, stales []catalogdb.ListStaleSearchSyncRow, metadataOnly bool) error {
	// Tags collection has no scalar metadata — only id + vectors.
	if metadataOnly {
		return b.clearStaleFlagsByRows(ctx, stales, true)
	}

	log.Printf("Syncing %d stale tag embeddings...", len(stales))

	ids := make([]string, len(stales))
	for i, s := range stales {
		ids[i] = s.RefID.String()
	}

	tags, err := b.storage.Querier().ListTag(ctx, catalogdb.ListTagParams{
		ID:    ids,
		Limit: null.Int32From(int32(len(ids))),
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch tags", err)
	}
	if len(tags) == 0 {
		return nil
	}

	texts := make([]string, len(tags))
	for i, t := range tags {
		texts[i] = buildTagEmbeddingText(t.ID, t.Name, t.Description.String)
	}

	embeddings, err := b.llm.Embed(ctx, texts)
	if err != nil {
		return sharedmodel.WrapErr("embed tags", err)
	}

	if err := b.upsertTags(ctx, tags, embeddings); err != nil {
		return sharedmodel.WrapErr("upsert tags", err)
	}

	// Tags use string IDs, not UUIDs — clear flags manually
	var updateArgs []catalogdb.UpdateBatchStaleSearchSyncParams
	for _, s := range stales {
		updateArgs = append(updateArgs, catalogdb.UpdateBatchStaleSearchSyncParams{
			RefType:          catalogdb.CatalogSearchSyncRefTypeTag,
			RefID:            s.RefID,
			IsStaleEmbedding: null.BoolFrom(false),
			IsStaleMetadata:  null.BoolFrom(false),
		})
	}
	return b.batchClearFlags(ctx, updateArgs)
}

// --- Flag clearing helpers ---

func clearStaleFlagsProducts(b *CatalogHandler, ctx context.Context, products []catalogmodel.ProductDetail, metadataOnly bool) error {
	var updateArgs []catalogdb.UpdateBatchStaleSearchSyncParams
	for _, p := range products {
		updateArgs = append(updateArgs, catalogdb.UpdateBatchStaleSearchSyncParams{
			RefType:          catalogdb.CatalogSearchSyncRefTypeProductSpu,
			RefID:            p.ID,
			IsStaleEmbedding: null.BoolFrom(metadataOnly),
			IsStaleMetadata:  null.BoolFrom(false),
		})
	}
	return b.batchClearFlags(ctx, updateArgs)
}

func clearStaleFlagsCategories(b *CatalogHandler, ctx context.Context, categories []catalogdb.CatalogCategory, metadataOnly bool) error {
	var updateArgs []catalogdb.UpdateBatchStaleSearchSyncParams
	for _, c := range categories {
		updateArgs = append(updateArgs, catalogdb.UpdateBatchStaleSearchSyncParams{
			RefType:          catalogdb.CatalogSearchSyncRefTypeCategory,
			RefID:            c.ID,
			IsStaleEmbedding: null.BoolFrom(metadataOnly),
			IsStaleMetadata:  null.BoolFrom(false),
		})
	}
	return b.batchClearFlags(ctx, updateArgs)
}

func (b *CatalogHandler) clearStaleFlagsByRows(ctx context.Context, stales []catalogdb.ListStaleSearchSyncRow, metadataOnly bool) error {
	var updateArgs []catalogdb.UpdateBatchStaleSearchSyncParams
	for _, s := range stales {
		updateArgs = append(updateArgs, catalogdb.UpdateBatchStaleSearchSyncParams{
			RefType:          s.RefType,
			RefID:            s.RefID,
			IsStaleEmbedding: null.BoolFrom(metadataOnly),
			IsStaleMetadata:  null.BoolFrom(false),
		})
	}
	return b.batchClearFlags(ctx, updateArgs)
}

func (b *CatalogHandler) batchClearFlags(ctx context.Context, args []catalogdb.UpdateBatchStaleSearchSyncParams) error {
	if len(args) == 0 {
		return nil
	}
	var updateErr error
	b.storage.Querier().UpdateBatchStaleSearchSync(ctx, args).Exec(func(i int, err error) {
		if err != nil {
			updateErr = err
		}
	})
	if updateErr != nil {
		return sharedmodel.WrapErr("clear stale flags", updateErr)
	}
	return nil
}

// --- Embedding text builders ---

type embeddingResult struct {
	dense  []float32
	sparse map[uint32]float32
}

// buildEmbeddingText produces natural-language text for product embedding.
func buildEmbeddingText(p catalogmodel.ProductDetail) string {
	var b strings.Builder

	b.WriteString(p.Name)

	if p.Category.Name != "" {
		b.WriteString(". ")
		b.WriteString(p.Category.Name)
	}

	if len(p.Tags) > 0 {
		b.WriteString(". ")
		b.WriteString(strings.Join(p.Tags, ", "))
	}

	attrSet := make(map[string][]string)
	for _, sku := range p.Skus {
		for _, attr := range sku.Attributes {
			attrSet[attr.Name] = appendUnique(attrSet[attr.Name], attr.Value)
		}
	}
	for name, values := range attrSet {
		b.WriteString(". ")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(strings.Join(values, " "))
	}

	for _, s := range p.Specifications {
		b.WriteString(". ")
		b.WriteString(s.Name)
		b.WriteString(" ")
		b.WriteString(s.Value)
	}

	desc := htmlutil.StripHTML(p.Description)
	if desc != "" {
		b.WriteString(". ")
		b.WriteString(desc)
	}

	return b.String()
}

func buildCategoryEmbeddingText(name, description string) string {
	if description == "" {
		return name
	}
	return name + ". " + description
}

func buildTagEmbeddingText(id, name, description string) string {
	var b strings.Builder
	b.WriteString(id)
	if name != "" {
		b.WriteString(". ")
		b.WriteString(name)
	}
	if description != "" {
		b.WriteString(". ")
		b.WriteString(description)
	}
	return b.String()
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
```

**Important:** This file uses `catalogdb.CatalogSearchSyncRefTypeCategory` and `catalogdb.CatalogSearchSyncRefTypeTag` — these only exist after Task 1 migration + Task 2 SQLC regeneration.

Also note: `ListTag` takes `ID` as `[]string` (tag IDs are strings, not UUIDs). The tag stale rows have `RefID` as `uuid.UUID` — we convert to string for the query, but the `search_sync` table stores tag IDs as UUID. **Check:** if tags use string IDs (VARCHAR primary key), the seeder will need to store a UUID in `search_sync.ref_id`. This is a design tension — tag IDs are slugs like `"eco-friendly"`, not UUIDs. The `search_sync.ref_id` column is UUID type. We'll need to handle this in Task 8 (seeder) by using a deterministic UUID derived from the tag ID string, or by changing the approach. For now, proceed with the implementation assuming we use `uuid.NewSHA1(uuid.NameSpaceURL, []byte(tagID))` to generate a deterministic UUID from the tag slug.

- [ ] **Step 2: Verify build**

```bash
go build ./internal/module/catalog/...
```

- [ ] **Step 3: Commit search.go + search_sync.go together**

```bash
git add internal/module/catalog/biz/search.go internal/module/catalog/biz/search_sync.go
git commit -m "rewrite search.go and search_sync.go, unified cron dispatch"
```

---

### Task 7: Add upsertCategories and upsertTags to search_milvus.go

**Files:**
- Modify: `internal/module/catalog/biz/search_milvus.go`

- [ ] **Step 1: Add upsertCategories**

Add to `internal/module/catalog/biz/search_milvus.go`:

```go
// upsertCategories upserts category vectors to the Milvus categories collection.
func (b *CatalogHandler) upsertCategories(ctx context.Context, categories []catalogdb.CatalogCategory, embeddings []llm.Embedding) error {
	if len(categories) == 0 {
		return nil
	}

	ids := make([]string, len(categories))
	denseVecs := make([][]float32, len(categories))
	sparseVecs := make([]entity.SparseEmbedding, len(categories))

	for i, c := range categories {
		ids[i] = c.ID.String()
		denseVecs[i] = embeddings[i].Dense
		if embeddings[i].Sparse != nil {
			sparseVecs[i] = mapToSparseEmbedding(embeddings[i].Sparse)
		} else {
			emptyEmb, _ := entity.NewSliceSparseEmbedding(nil, nil)
			sparseVecs[i] = emptyEmb
		}
	}

	cols := []column.Column{
		column.NewColumnVarChar("id", ids),
		column.NewColumnFloatVector("content_vector", ContentVectorDim, denseVecs),
		column.NewColumnSparseVectors("sparse_vector", sparseVecs),
	}

	_, err := b.milvus.Inner().Upsert(ctx, milvusclient.NewColumnBasedInsertOption(CollectionCategories, cols...))
	if err != nil {
		return sharedmodel.WrapErr("upsert categories", err)
	}
	return nil
}
```

- [ ] **Step 2: Add upsertTags**

Add to `internal/module/catalog/biz/search_milvus.go`:

```go
// upsertTags upserts tag vectors to the Milvus tags collection.
func (b *CatalogHandler) upsertTags(ctx context.Context, tags []catalogdb.CatalogTag, embeddings []llm.Embedding) error {
	if len(tags) == 0 {
		return nil
	}

	ids := make([]string, len(tags))
	denseVecs := make([][]float32, len(tags))
	sparseVecs := make([]entity.SparseEmbedding, len(tags))

	for i, t := range tags {
		ids[i] = t.ID
		denseVecs[i] = embeddings[i].Dense
		if embeddings[i].Sparse != nil {
			sparseVecs[i] = mapToSparseEmbedding(embeddings[i].Sparse)
		} else {
			emptyEmb, _ := entity.NewSliceSparseEmbedding(nil, nil)
			sparseVecs[i] = emptyEmb
		}
	}

	cols := []column.Column{
		column.NewColumnVarChar("id", ids),
		column.NewColumnFloatVector("content_vector", ContentVectorDim, denseVecs),
		column.NewColumnSparseVectors("sparse_vector", sparseVecs),
	}

	_, err := b.milvus.Inner().Upsert(ctx, milvusclient.NewColumnBasedInsertOption(CollectionTags, cols...))
	if err != nil {
		return sharedmodel.WrapErr("upsert tags", err)
	}
	return nil
}
```

- [ ] **Step 3: Add llm import**

Add `"shopnexus-server/internal/provider/llm"` to the imports of `search_milvus.go` (needed for `llm.Embedding` type in the new function signatures).

- [ ] **Step 4: Verify build**

```bash
go build ./internal/module/catalog/...
```

- [ ] **Step 5: Commit**

```bash
git add internal/module/catalog/biz/search_milvus.go
git commit -m "add upsertCategories and upsertTags to search_milvus.go"
```

---

### Task 8: Update Interface — Remove UpdateProducts

**Files:**
- Modify: `internal/module/catalog/biz/interface.go`

- [ ] **Step 1: Check for external callers of UpdateProducts**

```bash
grep -r "UpdateProducts" --include="*.go" . | grep -v "_test.go"
```

Confirm the only references are in `search.go` (now removed), `search_sync.go` (now rewritten), and possibly `restate_gen.go`. The interface method and its Restate proxy need to be removed.

- [ ] **Step 2: Remove from interface**

The `UpdateProducts` method was not in the `CatalogBiz` interface (it was an unexported Restate handler). Verify this:

```bash
grep "UpdateProducts" internal/module/catalog/biz/interface.go
```

If it's not in the interface, no interface change is needed. The `UpdateProductsParams` type and `UpdateProducts` method were in `search.go` — already removed in Task 5.

- [ ] **Step 3: Regenerate Restate proxy**

```bash
go generate ./internal/module/catalog/biz/
```

This regenerates `restate_gen.go` from the interface. Since `UpdateProducts` was not in the interface, this should be a no-op, but run it to confirm.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/module/catalog/biz/
git commit -m "remove UpdateProducts, regenerate Restate proxy"
```

---

### Task 9: Update Seeder — Add search_sync Rows for Categories and Tags

**Files:**
- Modify: `cmd/seed/helpers.go`

- [ ] **Step 1: Update seedCategories to create search_sync rows**

In `cmd/seed/helpers.go`, after creating each category in `seedCategories()`, also create a `search_sync` row. Add the import for the search_sync ref type and modify the loop:

In the `seedCategories` function, after each successful category creation (both `existing` and `created` paths), add:

```go
// Create search_sync entry for this category
_, syncErr := store.CreateSearchSync(ctx, catalogdb.CreateSearchSyncParams{
    RefType:          catalogdb.CatalogSearchSyncRefTypeCategory,
    RefID:            categoryID, // the UUID from existing or created
    IsStaleEmbedding: true,
    IsStaleMetadata:  true,
    DateCreated:      time.Now(),
    DateUpdated:      time.Now(),
})
if syncErr != nil {
    log.Printf("  Warning: failed to create search_sync for category %s: %v", cat.Name, syncErr)
}
```

Add `"time"` to imports.

- [ ] **Step 2: Add search_sync rows for tags in createTags**

In the `createTags` function, after each successful tag creation + product association, also upsert a search_sync row for the tag. Since tags use string IDs (VARCHAR), we need a deterministic UUID. Use `uuid.NewSHA1`:

After the tag is created/found and the `CreateProductSpuTag` call, add:

```go
// Ensure search_sync row exists for this tag
tagUUID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(tagID))
_, _ = store.CreateSearchSync(ctx, catalogdb.CreateSearchSyncParams{
    RefType:          catalogdb.CatalogSearchSyncRefTypeTag,
    RefID:            tagUUID,
    IsStaleEmbedding: true,
    IsStaleMetadata:  true,
    DateCreated:      time.Now(),
    DateUpdated:      time.Now(),
})
```

This is idempotent — the unique constraint `(ref_type, ref_id)` will reject duplicates, and we silently ignore the error.

- [ ] **Step 3: Update syncTags to use the same UUID derivation**

Back in `internal/module/catalog/biz/search_sync.go`, the `syncTags` function receives stale rows with `RefID` as UUID. It needs to map these back to tag string IDs. Since we used `uuid.NewSHA1(uuid.NameSpaceURL, []byte(tagID))` in the seeder, we can't reverse it. Instead, query tags by fetching all and matching UUIDs, or store the tag ID alongside.

**Simpler approach:** Change `syncTags` to query the `search_sync` table for Tag rows, get the `ref_id` UUIDs, then query the tags table with `ListTag` using a broader approach. Actually, the cleanest solution: add a `number` or similar mapping column. But that's over-engineering.

**Pragmatic solution:** Since tags are few, just fetch all tags and create a UUID→tag map:

```go
func (b *CatalogHandler) syncTags(ctx context.Context, stales []catalogdb.ListStaleSearchSyncRow, metadataOnly bool) error {
	if metadataOnly {
		return b.clearStaleFlagsByRows(ctx, stales, true)
	}

	log.Printf("Syncing %d stale tag embeddings...", len(stales))

	// Build UUID→stale map
	staleMap := make(map[uuid.UUID]catalogdb.ListStaleSearchSyncRow, len(stales))
	for _, s := range stales {
		staleMap[s.RefID] = s
	}

	// Fetch all tags (tags are small, typically < 1000)
	allTags, err := b.storage.Querier().ListTag(ctx, catalogdb.ListTagParams{
		Limit: null.Int32From(10000),
	})
	if err != nil {
		return sharedmodel.WrapErr("fetch tags", err)
	}

	// Match tags to stale rows via deterministic UUID
	var matchedTags []catalogdb.CatalogTag
	var matchedStales []catalogdb.ListStaleSearchSyncRow
	for _, t := range allTags {
		tagUUID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(t.ID))
		if s, ok := staleMap[tagUUID]; ok {
			matchedTags = append(matchedTags, t)
			matchedStales = append(matchedStales, s)
		}
	}

	if len(matchedTags) == 0 {
		return nil
	}

	texts := make([]string, len(matchedTags))
	for i, t := range matchedTags {
		texts[i] = buildTagEmbeddingText(t.ID, t.Name, t.Description.String)
	}

	embeddings, err := b.llm.Embed(ctx, texts)
	if err != nil {
		return sharedmodel.WrapErr("embed tags", err)
	}

	if err := b.upsertTags(ctx, matchedTags, embeddings); err != nil {
		return sharedmodel.WrapErr("upsert tags", err)
	}

	return b.clearStaleFlagsByRows(ctx, matchedStales, false)
}
```

- [ ] **Step 4: Verify build**

```bash
go build ./cmd/seed/ && go build ./internal/module/catalog/...
```

- [ ] **Step 5: Commit**

```bash
git add cmd/seed/helpers.go internal/module/catalog/biz/search_sync.go
git commit -m "add search_sync rows for categories and tags in seeder"
```

---

### Task 10: Final Verification — Full Build + Cleanup

**Files:**
- All modified files

- [ ] **Step 1: Full build**

```bash
go build ./...
```

Fix any compilation errors.

- [ ] **Step 2: Check for dead code**

```bash
grep -r "UpdateProductsParams\|updateStaleProducts\|syncProductData" --include="*.go" internal/
```

These should have zero hits — they belonged to the old flow.

- [ ] **Step 3: Check no orphaned imports**

```bash
go vet ./internal/module/catalog/...
```

- [ ] **Step 4: Verify file organization matches spec**

Confirm each file contains only what the spec says:
- `search.go` — SearchParams, buildFilterExpr, Search, GetRecommendations, mapToSparseEmbedding, toMilvusStringList
- `search_sync.go` — SetupCron, startSyncCron, syncStaleEntities, syncProducts, syncCategories, syncTags, embedding text builders, flag clearing helpers
- `search_milvus.go` — upsertProducts, upsertCategories, upsertTags, getProductVectors, getProductAllVectors, getAccountInterests, upsertAccountInterests
- `search_events.go` — AddInteraction, ProcessEvents, InterleaveShuffle, account helpers

- [ ] **Step 5: Commit any cleanup**

```bash
git add -A
git commit -m "clean up search sync redesign"
```

package catalogbiz

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/google/uuid"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"

	"shopnexus-server/internal/infras/milvus"
	accountmodel "shopnexus-server/internal/module/account/model"
	analyticmodel "shopnexus-server/internal/module/analytic/model"
	catalogmodel "shopnexus-server/internal/module/catalog/model"
	catalogutil "shopnexus-server/internal/module/catalog/util"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

type SearchParams struct {
	sharedmodel.PaginationParams
	Collection string
	Query      string
}

// Search performs hybrid dense+sparse vector search against the product collection.
func (b *CatalogHandler) Search(ctx restate.Context, params SearchParams) ([]catalogmodel.ProductRecommend, error) {
	// 1. Get embeddings from embedding service
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

	// 2. Build search requests
	denseReq := milvusclient.NewAnnRequest("content_vector", limit, entity.FloatVector(emb.Dense)).
		WithOffset(offset)

	var results []milvus.SearchResult

	if emb.Sparse != nil {
		// Hybrid dense+sparse search
		sparseVec := mapToSparseEmbedding(emb.Sparse)
		sparseReq := milvusclient.NewAnnRequest("sparse_vector", limit, sparseVec).
			WithOffset(offset)

		reranker := milvusclient.NewWeightedReranker([]float64{
			float64(b.denseWeight),
			float64(b.sparseWeight),
		})

		results, err = b.milvus.HybridSearch(ctx, CollectionProducts,
			limit, reranker, []string{"id"},
			denseReq, sparseReq,
		)
	} else {
		// Dense-only search (provider does not return sparse vectors)
		results, err = b.milvus.Search(ctx, CollectionProducts,
			limit, []entity.Vector{entity.FloatVector(emb.Dense)},
			"content_vector", []string{"id"},
		)
	}
	if err != nil {
		return nil, sharedmodel.WrapErr("search", err)
	}

	// 4. Convert results
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

// GetRecommendations returns product recommendations based on the user's interest vectors.
func (b *CatalogHandler) GetRecommendations(ctx restate.Context, params GetRecommendationsParams) ([]catalogmodel.ProductRecommend, error) {
	accountID := params.Account.ID.String()

	// 1. Query account interest vectors from Milvus
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

	// 2. Build ANN search request per active interest slot
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

	// 3. Normalize weights so the maximum is 1.0
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

	// 4. Hybrid search with weighted ranker
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

// ProcessEvents updates account interest vectors in Milvus based on analytic interaction events.
func (b *CatalogHandler) ProcessEvents(ctx restate.Context, events []analyticmodel.Interaction) error {
	if len(events) == 0 {
		return nil
	}

	// 1. Collect unique product IDs
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

	// 2. Fetch product content vectors from Milvus
	itemVectors, err := b.getProductVectors(ctx, itemIDs)
	if err != nil {
		return sharedmodel.WrapErr("get product vectors", err)
	}

	// 3. Group events by account
	accountEvents := make(map[string][]analyticmodel.Interaction)
	for _, e := range events {
		if !e.AccountID.Valid {
			continue
		}
		aid := e.AccountID.UUID.String()
		accountEvents[aid] = append(accountEvents[aid], e)
	}

	// 4. Fetch existing account interests
	accountIDs := make([]string, 0, len(accountEvents))
	for id := range accountEvents {
		accountIDs = append(accountIDs, id)
	}
	existingAccounts, err := b.getAccountInterests(ctx, accountIDs)
	if err != nil {
		return sharedmodel.WrapErr("get account interests", err)
	}

	// 5. Process each account's events
	for accountID, acctEvents := range accountEvents {
		interests, strengths := catalogutil.DefaultInterests(ContentVectorDim)
		if existing, ok := existingAccounts[accountID]; ok {
			interests = existing.interests
			strengths = existing.strengths
		}

		// Aggregate event weights per product
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

		// 6. Upsert updated account
		if err := b.upsertAccountInterests(ctx, accountID, acctEvents[0].AccountNumber, interests, strengths); err != nil {
			return sharedmodel.WrapErr(fmt.Sprintf("upsert account %s", accountID), err)
		}
	}

	return nil
}

type UpdateProductsParams struct {
	Products     []catalogmodel.ProductDetail `validate:"required"`
	MetadataOnly bool
}

// UpdateProducts upserts product data and embeddings into the Milvus search index.
func (b *CatalogHandler) UpdateProducts(ctx restate.Context, params UpdateProductsParams) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate update products", err)
	}

	// 1. Get embeddings if needed
	var embeddingMap map[string]embeddingResult
	if !params.MetadataOnly {
		texts := make([]string, len(params.Products))
		for i, p := range params.Products {
			texts[i] = p.Name + " " + p.Description
		}
		embeddings, err := b.llm.Embed(ctx, texts)
		if err != nil {
			return sharedmodel.WrapErr("embed products", err)
		}
		embeddingMap = make(map[string]embeddingResult, len(params.Products))
		for i, p := range params.Products {
			embeddingMap[p.ID.String()] = embeddingResult{
				dense:  embeddings[i].Dense,
				sparse: embeddings[i].Sparse,
			}
		}
	}

	// 2. Upsert to Milvus
	if err := b.upsertProducts(ctx, params.Products, embeddingMap, params.MetadataOnly); err != nil {
		return sharedmodel.WrapErr("upsert products", err)
	}

	return nil
}

// Helper types

type embeddingResult struct {
	dense  []float32
	sparse map[uint32]float32
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

// AddInteraction buffers an analytic interaction event and flushes the batch when full.
func (b *CatalogHandler) AddInteraction(ctx restate.Context, params analyticmodel.Interaction) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// buffer the event
	// WIP: storing buffer in memory kinda sucks, but good enough for now
	b.buffer = append(b.buffer, params)

	// if reached batch size, process events
	if len(b.buffer) >= b.batchSize {
		toInsert := b.buffer
		b.buffer = make([]analyticmodel.Interaction, 0, b.batchSize) // reset buffer

		// Refresh customer feeds
		if err := b.ProcessEvents(ctx, toInsert); err != nil {
			return err
		}

		// Remove old recommendations for all affected accounts
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

// InterleaveShuffle splits each input slice into numParts chunks,
// combines the chunks for each part, and shuffles within each part.
// This ensures every part contains a proportional mix of all input slices.
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

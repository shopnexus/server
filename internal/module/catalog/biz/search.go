package catalogbiz

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"

	accountmodel "shopnexus-remastered/internal/module/account/model"
	analyticmodel "shopnexus-remastered/internal/module/analytic/model"
	catalogmodel "shopnexus-remastered/internal/module/catalog/model"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"
)

type SearchParams struct {
	commonmodel.PaginationParams
	Collection string
	Query      string
}

func (b *CatalogBiz) Search(ctx context.Context, params SearchParams) ([]catalogmodel.ProductRecommend, error) {
	// 1. Get embeddings from embedding service
	embeddings, err := b.embedding.Embed(ctx, []string{params.Query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	emb := embeddings[0]

	pag := params.Constrain()
	limit := int(pag.Limit.Int32)
	offset := 0
	if pag.Offset().Valid {
		offset = int(pag.Offset().Int32)
	}

	// 2. Build hybrid search requests
	denseReq := milvusclient.NewAnnRequest("content_vector", limit, entity.FloatVector(emb.Dense)).
		WithOffset(offset)

	sparseVec := mapToSparseEmbedding(emb.Sparse)
	sparseReq := milvusclient.NewAnnRequest("sparse_vector", limit, sparseVec).
		WithOffset(offset)

	// 3. Hybrid search with weighted ranker
	reranker := milvusclient.NewWeightedReranker([]float64{
		float64(b.denseWeight),
		float64(b.sparseWeight),
	})

	results, err := b.milvus.HybridSearch(ctx, CollectionProducts,
		limit, reranker, []string{"id"},
		denseReq, sparseReq,
	)
	if err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
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

func (b *CatalogBiz) GetRecommendations(ctx context.Context, params GetRecommendationsParams) ([]catalogmodel.ProductRecommend, error) {
	accountID := params.Account.ID.String()

	// 1. Query account interest vectors from Milvus
	rs, err := b.milvus.Query(ctx, CollectionAccounts,
		fmt.Sprintf("id == '%s'", accountID),
		accountOutputFields(),
	)
	if err != nil {
		return nil, fmt.Errorf("query account: %w", err)
	}
	if rs.ResultCount == 0 {
		return nil, nil
	}

	// 2. Build ANN search request per active interest slot
	var searchReqs []*milvusclient.AnnRequest
	var weights []float64

	for i := 1; i <= numInterests; i++ {
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
		return nil, fmt.Errorf("recommend search: %w", err)
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

func (b *CatalogBiz) ProcessEvents(ctx context.Context, events []analyticmodel.Interaction) error {
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
		return fmt.Errorf("get product vectors: %w", err)
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
		return fmt.Errorf("get account interests: %w", err)
	}

	// 5. Process each account's events
	for accountID, acctEvents := range accountEvents {
		interests, strengths := defaultInterests(ContentVectorDim)
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
				assignPositive(interests, strengths, productVec, weight)
			} else if weight < 0 {
				assignNegative(interests, strengths, productVec, weight)
			}
		}

		// 6. Upsert updated account
		if err := b.upsertAccountInterests(ctx, accountID, acctEvents[0].AccountNumber, interests, strengths); err != nil {
			return fmt.Errorf("upsert account %s: %w", accountID, err)
		}
	}

	return nil
}

type UpdateProductsParams struct {
	Products     []catalogmodel.ProductDetail `validate:"required"`
	MetadataOnly bool
}

func (b *CatalogBiz) UpdateProducts(ctx context.Context, params UpdateProductsParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	// 1. Get embeddings if needed
	var embeddingMap map[string]embeddingResult
	if !params.MetadataOnly {
		texts := make([]string, len(params.Products))
		for i, p := range params.Products {
			texts[i] = p.Name + " " + p.Description
		}
		embeddings, err := b.embedding.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed products: %w", err)
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
		return fmt.Errorf("upsert products: %w", err)
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
		weights[e.RefID] += getEventWeight(strings.ToLower(e.EventType))
	}
	return weights
}

func accountOutputFields() []string {
	fields := make([]string, 0, 1+numInterests*2)
	fields = append(fields, "id")
	for i := 1; i <= numInterests; i++ {
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

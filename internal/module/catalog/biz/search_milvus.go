package catalogbiz

import (
	"context"
	"fmt"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"

	catalogmodel "shopnexus-server/internal/module/catalog/model"
	catalogutil "shopnexus-server/internal/module/catalog/util"
)

// getProductVectors fetches content_vector for the given product IDs from Milvus.
func (b *CatalogBiz) getProductVectors(ctx context.Context, ids []string) (map[string][]float32, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	expr := fmt.Sprintf("id in %s", toMilvusStringList(ids))
	rs, err := b.milvus.Query(ctx, CollectionProducts, expr, []string{"id", "content_vector"})
	if err != nil {
		return nil, err
	}

	idCol := rs.GetColumn("id")
	vecCol := rs.GetColumn("content_vector")
	if idCol == nil || vecCol == nil {
		return nil, nil
	}

	result := make(map[string][]float32, rs.ResultCount)
	for i := 0; i < rs.ResultCount; i++ {
		id, err := idCol.GetAsString(i)
		if err != nil {
			continue
		}
		vecAny, err := vecCol.Get(i)
		if err != nil {
			continue
		}
		if vec, ok := vecAny.(entity.FloatVector); ok {
			result[id] = []float32(vec)
		}
	}
	return result, nil
}

// getAccountInterests fetches interest vectors and strengths for the given account IDs.
func (b *CatalogBiz) getAccountInterests(ctx context.Context, ids []string) (map[string]accountInterests, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	expr := fmt.Sprintf("id in %s", toMilvusStringList(ids))
	rs, err := b.milvus.Query(ctx, CollectionAccounts, expr, accountOutputFields())
	if err != nil {
		return nil, err
	}

	idCol := rs.GetColumn("id")
	if idCol == nil {
		return nil, nil
	}

	result := make(map[string]accountInterests, rs.ResultCount)
	for i := 0; i < rs.ResultCount; i++ {
		id, err := idCol.GetAsString(i)
		if err != nil {
			continue
		}

		interests := make([][]float32, catalogutil.NumInterests)
		strengths := make([]float32, catalogutil.NumInterests)
		for j := 0; j < catalogutil.NumInterests; j++ {
			vecCol := rs.GetColumn(fmt.Sprintf("interest_%d", j+1))
			strCol := rs.GetColumn(fmt.Sprintf("strength_%d", j+1))

			if vecCol != nil {
				vecAny, err := vecCol.Get(i)
				if err == nil {
					if vec, ok := vecAny.(entity.FloatVector); ok {
						interests[j] = []float32(vec)
					}
				}
			}
			if interests[j] == nil {
				interests[j] = make([]float32, ContentVectorDim)
			}

			if strCol != nil {
				s, err := strCol.GetAsDouble(i)
				if err == nil {
					strengths[j] = float32(s)
				}
			}
		}
		result[id] = accountInterests{interests: interests, strengths: strengths}
	}
	return result, nil
}

// upsertAccountInterests upserts an account's interest vectors and strengths to Milvus.
func (b *CatalogBiz) upsertAccountInterests(ctx context.Context, accountID string, accountNumber int64, interests [][]float32, strengths []float32) error {
	cols := []column.Column{
		column.NewColumnVarChar("id", []string{accountID}),
		column.NewColumnInt64("number", []int64{accountNumber}),
	}
	for i := 0; i < catalogutil.NumInterests; i++ {
		cols = append(cols, column.NewColumnFloatVector(fmt.Sprintf("interest_%d", i+1), ContentVectorDim, [][]float32{interests[i]}))
		cols = append(cols, column.NewColumnFloat(fmt.Sprintf("strength_%d", i+1), []float32{strengths[i]}))
	}

	_, err := b.milvus.Inner().Upsert(ctx, milvusclient.NewColumnBasedInsertOption(CollectionAccounts, cols...))
	return err
}

// getProductAllVectors fetches content_vector and sparse_vector for the given product IDs from Milvus.
func (b *CatalogBiz) getProductAllVectors(ctx context.Context, ids []string) (map[string]existingVectors, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	expr := fmt.Sprintf("id in %s", toMilvusStringList(ids))
	rs, err := b.milvus.Query(ctx, CollectionProducts, expr, []string{"id", "content_vector", "sparse_vector"})
	if err != nil {
		return nil, err
	}

	idCol := rs.GetColumn("id")
	denseCol := rs.GetColumn("content_vector")
	sparseCol := rs.GetColumn("sparse_vector")
	if idCol == nil {
		return nil, nil
	}

	result := make(map[string]existingVectors, rs.ResultCount)
	for i := 0; i < rs.ResultCount; i++ {
		id, err := idCol.GetAsString(i)
		if err != nil {
			continue
		}
		ev := existingVectors{}
		if denseCol != nil {
			if vecAny, err := denseCol.Get(i); err == nil {
				if vec, ok := vecAny.(entity.FloatVector); ok {
					ev.dense = []float32(vec)
				}
			}
		}
		if sparseCol != nil {
			if sparseAny, err := sparseCol.Get(i); err == nil {
				if sv, ok := sparseAny.(entity.SparseEmbedding); ok {
					ev.sparse = sv
				}
			}
		}
		result[id] = ev
	}
	return result, nil
}

type existingVectors struct {
	dense  []float32
	sparse entity.SparseEmbedding
}

// upsertProducts upserts product data (and optionally vectors) to Milvus.
func (b *CatalogBiz) upsertProducts(ctx context.Context, products []catalogmodel.ProductDetail, embeddings map[string]embeddingResult, metadataOnly bool) error {
	if len(products) == 0 {
		return nil
	}

	// For metadata-only updates, fetch existing vectors since Milvus Upsert requires all fields.
	var existingVecMap map[string]existingVectors
	if metadataOnly {
		productIDs := make([]string, len(products))
		for i, p := range products {
			productIDs[i] = p.ID.String()
		}
		var err error
		existingVecMap, err = b.getProductAllVectors(ctx, productIDs)
		if err != nil {
			return fmt.Errorf("fetch existing vectors: %w", err)
		}
	}

	ids := make([]string, 0, len(products))
	numbers := make([]int64, 0, len(products))
	names := make([]string, 0, len(products))
	descriptions := make([]string, 0, len(products))
	brands := make([]string, 0, len(products))
	categories := make([]string, 0, len(products))
	isActives := make([]bool, 0, len(products))
	ratings := make([]float32, 0, len(products))
	skusJSON := make([][]byte, 0, len(products))
	specsJSON := make([][]byte, 0, len(products))
	denseVecs := make([][]float32, 0, len(products))
	sparseVecs := make([]entity.SparseEmbedding, 0, len(products))

	for _, p := range products {
		pid := p.ID.String()

		ids = append(ids, pid)
		numbers = append(numbers, 0) // number not in ProductDetail; use 0
		names = append(names, p.Name)
		desc := p.Description
		if len(desc) > 10240 {
			desc = desc[:10240]
		}
		descriptions = append(descriptions, desc)
		brands = append(brands, p.Brand.Name)
		categories = append(categories, p.Category.Name)
		isActives = append(isActives, p.IsActive)
		ratings = append(ratings, float32(p.Rating.Score))

		skuBytes, _ := sonic.Marshal(p.Skus)
		skusJSON = append(skusJSON, skuBytes)
		specBytes, _ := sonic.Marshal(p.Specifications)
		specsJSON = append(specsJSON, specBytes)

		if metadataOnly {
			if ev, ok := existingVecMap[pid]; ok {
				denseVecs = append(denseVecs, ev.dense)
				sparseVecs = append(sparseVecs, ev.sparse)
			} else {
				// Product not yet in Milvus; use zero vectors
				denseVecs = append(denseVecs, make([]float32, ContentVectorDim))
				emptyEmb, _ := entity.NewSliceSparseEmbedding(nil, nil)
				sparseVecs = append(sparseVecs, emptyEmb)
			}
		} else {
			emb := embeddings[pid]
			denseVecs = append(denseVecs, emb.dense)
			sparseVecs = append(sparseVecs, mapToSparseEmbedding(emb.sparse))
		}
	}

	cols := []column.Column{
		column.NewColumnVarChar("id", ids),
		column.NewColumnInt64("number", numbers),
		column.NewColumnVarChar("name", names),
		column.NewColumnVarChar("description", descriptions),
		column.NewColumnVarChar("brand", brands),
		column.NewColumnVarChar("category", categories),
		column.NewColumnBool("is_active", isActives),
		column.NewColumnFloat("rating", ratings),
		column.NewColumnJSONBytes("skus", skusJSON),
		column.NewColumnJSONBytes("specifications", specsJSON),
		column.NewColumnFloatVector("content_vector", ContentVectorDim, denseVecs),
		column.NewColumnSparseVectors("sparse_vector", sparseVecs),
	}

	_, err := b.milvus.Inner().Upsert(ctx, milvusclient.NewColumnBasedInsertOption(CollectionProducts, cols...))
	return err
}

// toMilvusStringList formats a string slice as a Milvus filter expression list: ['a','b']
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

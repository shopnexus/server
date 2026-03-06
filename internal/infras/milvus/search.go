package milvus

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

// SearchResult holds a single hit from a vector search.
type SearchResult struct {
	ID    string
	Score float32
}

// HybridSearch performs a multi-vector hybrid search with reranking.
// It delegates to the SDK's HybridSearch and flattens the first query's results.
func (c *Client) HybridSearch(
	ctx context.Context,
	collection string,
	limit int,
	reranker milvusclient.Reranker,
	outputFields []string,
	requests ...*milvusclient.AnnRequest,
) ([]SearchResult, error) {
	opt := milvusclient.NewHybridSearchOption(collection, limit, requests...).
		WithReranker(reranker).
		WithOutputFields(outputFields...)

	resultSets, err := c.inner.HybridSearch(ctx, opt)
	if err != nil {
		return nil, fmt.Errorf("milvus HybridSearch %q: %w", collection, err)
	}

	return extractResults(resultSets)
}

// Search performs a single-vector ANN search.
func (c *Client) Search(
	ctx context.Context,
	collection string,
	limit int,
	vectors []entity.Vector,
	annField string,
	outputFields []string,
) ([]SearchResult, error) {
	opt := milvusclient.NewSearchOption(collection, limit, vectors).
		WithANNSField(annField).
		WithOutputFields(outputFields...)

	resultSets, err := c.inner.Search(ctx, opt)
	if err != nil {
		return nil, fmt.Errorf("milvus Search %q: %w", collection, err)
	}

	return extractResults(resultSets)
}

// extractResults converts the SDK ResultSet slice into our SearchResult slice.
// It takes only the first query's results (nq=1 is the common case).
func extractResults(resultSets []milvusclient.ResultSet) ([]SearchResult, error) {
	if len(resultSets) == 0 {
		return nil, nil
	}

	rs := resultSets[0]
	if rs.Err != nil {
		return nil, rs.Err
	}

	results := make([]SearchResult, 0, rs.ResultCount)
	for i := 0; i < rs.ResultCount; i++ {
		idStr, err := rs.IDs.GetAsString(i)
		if err != nil {
			// Fallback: try to get as generic value and format.
			val, err2 := rs.IDs.Get(i)
			if err2 != nil {
				return nil, fmt.Errorf("failed to read ID at index %d: %w", i, err2)
			}
			idStr = fmt.Sprintf("%v", val)
		}

		var score float32
		if i < len(rs.Scores) {
			score = rs.Scores[i]
		}

		results = append(results, SearchResult{
			ID:    idStr,
			Score: score,
		})
	}

	return results, nil
}

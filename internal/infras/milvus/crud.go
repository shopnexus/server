package milvus

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

// Query executes a filtered query (no vector search) and returns the result set.
func (c *Client) Query(
	ctx context.Context,
	collection string,
	filter string,
	outputFields []string,
) (milvusclient.ResultSet, error) {
	opt := milvusclient.NewQueryOption(collection).
		WithFilter(filter).
		WithOutputFields(outputFields...)

	rs, err := c.inner.Query(ctx, opt)
	if err != nil {
		return rs, fmt.Errorf("milvus Query %q: %w", collection, err)
	}
	return rs, nil
}

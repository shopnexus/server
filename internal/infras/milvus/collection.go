package milvus

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

// IndexDef describes an index to create on a collection field.
type IndexDef struct {
	FieldName  string
	IndexType  index.IndexType
	MetricType entity.MetricType
}

// EnsureCollection checks whether a collection exists and creates it if missing.
// After creation (or if it already exists), the collection is loaded into memory.
func (c *Client) EnsureCollection(ctx context.Context, name string, schema *entity.Schema, indexes []IndexDef) error {
	has, err := c.inner.HasCollection(ctx, milvusclient.NewHasCollectionOption(name))
	if err != nil {
		return fmt.Errorf("milvus HasCollection %q: %w", name, err)
	}

	if !has {
		// Build index options for each definition.
		indexOpts := make([]milvusclient.CreateIndexOption, 0, len(indexes))
		for _, def := range indexes {
			idx := buildIndex(def)
			indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(name, def.FieldName, idx))
		}

		// Create the collection with schema and indexes.
		createOpt := milvusclient.NewCreateCollectionOption(name, schema).
			WithIndexOptions(indexOpts...)

		if err := c.inner.CreateCollection(ctx, createOpt); err != nil {
			return fmt.Errorf("milvus CreateCollection %q: %w", name, err)
		}
		slog.Info("created Milvus collection", "name", name)
	}

	// Load the collection into memory so it is searchable.
	loadTask, err := c.inner.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(name))
	if err != nil {
		return fmt.Errorf("milvus LoadCollection %q: %w", name, err)
	}
	if err := loadTask.Await(ctx); err != nil {
		return fmt.Errorf("milvus LoadCollection await %q: %w", name, err)
	}
	slog.Info("loaded Milvus collection", "name", name)

	return nil
}

// buildIndex returns a Milvus Index based on the IndexDef.
// Sparse vector fields use SPARSE_INVERTED_INDEX; all others use AUTOINDEX.
func buildIndex(def IndexDef) index.Index {
	switch def.IndexType {
	case index.SparseInverted:
		return index.NewSparseInvertedIndex(def.MetricType, 0)
	case index.SparseWAND:
		return index.NewSparseWANDIndex(def.MetricType, 0)
	default:
		// AUTOINDEX works for dense float/binary vectors and scalars.
		return index.NewAutoIndex(def.MetricType)
	}
}

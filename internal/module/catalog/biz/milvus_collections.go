package catalogbiz

import (
	"context"
	"fmt"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"

	"shopnexus-remastered/internal/infras/milvus"
)

const (
	CollectionProducts = "products"
	CollectionAccounts = "accounts"
	ContentVectorDim   = 1024 // BGE-M3 dense dimension
)

func productsSchema() *entity.Schema {
	return entity.NewSchema().
		WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(36).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName("number").WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName("name").WithDataType(entity.FieldTypeVarChar).WithMaxLength(1024)).
		WithField(entity.NewField().WithName("description").WithDataType(entity.FieldTypeVarChar).WithMaxLength(10240)).
		WithField(entity.NewField().WithName("brand").WithDataType(entity.FieldTypeVarChar).WithMaxLength(256)).
		WithField(entity.NewField().WithName("category").WithDataType(entity.FieldTypeVarChar).WithMaxLength(256)).
		WithField(entity.NewField().WithName("is_active").WithDataType(entity.FieldTypeBool)).
		WithField(entity.NewField().WithName("rating").WithDataType(entity.FieldTypeFloat)).
		WithField(entity.NewField().WithName("skus").WithDataType(entity.FieldTypeJSON)).
		WithField(entity.NewField().WithName("specifications").WithDataType(entity.FieldTypeJSON)).
		WithField(entity.NewField().WithName("sparse_vector").WithDataType(entity.FieldTypeSparseVector)).
		WithField(entity.NewField().WithName("content_vector").WithDataType(entity.FieldTypeFloatVector).WithDim(ContentVectorDim))
}

func productsIndexes() []milvus.IndexDef {
	return []milvus.IndexDef{
		{FieldName: "sparse_vector", IndexType: index.SparseInverted, MetricType: entity.IP},
		{FieldName: "content_vector", IndexType: index.AUTOINDEX, MetricType: entity.COSINE},
	}
}

func accountsSchema() *entity.Schema {
	schema := entity.NewSchema().
		WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(36).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName("number").WithDataType(entity.FieldTypeInt64))
	for i := 1; i <= numInterests; i++ {
		schema.WithField(entity.NewField().WithName(fmt.Sprintf("interest_%d", i)).WithDataType(entity.FieldTypeFloatVector).WithDim(ContentVectorDim))
		schema.WithField(entity.NewField().WithName(fmt.Sprintf("strength_%d", i)).WithDataType(entity.FieldTypeFloat))
	}
	return schema
}

func accountsIndexes() []milvus.IndexDef {
	indexes := make([]milvus.IndexDef, 0, numInterests)
	for i := 1; i <= numInterests; i++ {
		indexes = append(indexes, milvus.IndexDef{
			FieldName:  fmt.Sprintf("interest_%d", i),
			IndexType:  index.AUTOINDEX,
			MetricType: entity.COSINE,
		})
	}
	return indexes
}

// SetupMilvusCollections creates the products and accounts collections if they don't exist.
func (b *CatalogBiz) SetupMilvusCollections(ctx context.Context) error {
	if err := b.milvus.EnsureCollection(ctx, CollectionProducts, productsSchema(), productsIndexes()); err != nil {
		return fmt.Errorf("setup products collection: %w", err)
	}
	if err := b.milvus.EnsureCollection(ctx, CollectionAccounts, accountsSchema(), accountsIndexes()); err != nil {
		return fmt.Errorf("setup accounts collection: %w", err)
	}
	return nil
}

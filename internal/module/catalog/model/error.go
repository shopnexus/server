package catalogmodel

import sharedmodel "shopnexus-server/internal/shared/model"

// Sentinel errors for the catalog module.
var (
	ErrProductNotFound    = sharedmodel.NewError("catalog.product_not_found", "The requested product could not be found")
	ErrSkuNotBelongToSpu  = sharedmodel.NewError("catalog.sku_not_belong_to_spu", "The selected SKU does not belong to this product")
	ErrNoEmbeddingsResult = sharedmodel.NewError("catalog.no_embeddings", "No embeddings returned for the query")
)

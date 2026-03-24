package catalogmodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the catalog module.
var (
	ErrProductNotFound    = sharedmodel.NewError(http.StatusNotFound, "The requested product could not be found")
	ErrSkuNotBelongToSpu  = sharedmodel.NewError(http.StatusBadRequest, "The selected SKU does not belong to this product")
	ErrNoEmbeddingsResult = sharedmodel.NewError(http.StatusNotFound, "No embeddings returned for the query")
)

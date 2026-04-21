package catalogmodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the catalog module.
var (
	ErrProductNotFound   = sharedmodel.NewError(http.StatusNotFound, "product_not_found", "The requested product could not be found")
	ErrSkuNotBelongToSpu = sharedmodel.NewError(
		http.StatusBadRequest,
		"sku_not_belong_to_spu",
		"The selected SKU does not belong to this product",
	)
	ErrNoEmbeddingsResult   = sharedmodel.NewError(http.StatusNotFound, "no_embeddings_result", "No embeddings returned for the query")
	ErrMustPurchaseToReview = sharedmodel.NewError(
		http.StatusForbidden,
		"must_purchase_to_review",
		"You must have a completed order for this product before leaving a review",
	)
	ErrOrderAlreadyReviewed = sharedmodel.NewError(
		http.StatusConflict,
		"order_already_reviewed",
		"You have already reviewed this product for this order",
	)
)

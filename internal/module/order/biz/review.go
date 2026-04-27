package orderbiz

import (
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

// HasPurchasedProduct checks if an account has a successful order containing any of the given SKU IDs.
func (b *OrderHandler) HasPurchasedProduct(ctx restate.Context, params HasPurchasedProductParams) (bool, error) {
	if err := validator.Validate(params); err != nil {
		return false, sharedmodel.WrapErr("validate has purchased product", err)
	}

	return b.storage.Querier().HasPurchasedSku(ctx, orderdb.HasPurchasedSkuParams{
		AccountID: params.AccountID,
		SkuIds:    params.SkuIDs,
	})
}

// ListReviewableOrders returns successful orders that contain items matching the given SKU IDs.
func (b *OrderHandler) ListReviewableOrders(
	ctx restate.Context,
	params ListReviewableOrdersParams,
) ([]ReviewableOrder, error) {
	if err := validator.Validate(params); err != nil {
		return nil, sharedmodel.WrapErr("validate list reviewable orders", err)
	}

	orders, err := b.storage.Querier().ListSuccessOrdersBySkus(ctx, orderdb.ListSuccessOrdersBySkusParams{
		BuyerID: params.AccountID,
		SkuIds:  params.SkuIDs,
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db list reviewable orders", err)
	}

	result := make([]ReviewableOrder, len(orders))
	for i, o := range orders {
		result[i] = ReviewableOrder{
			ID:          o.ID,
			DateCreated: o.DateCreated,
		}
	}
	return result, nil
}

// ValidateOrderForReview checks if a specific order is eligible for review.
func (b *OrderHandler) ValidateOrderForReview(ctx restate.Context, params ValidateOrderForReviewParams) (bool, error) {
	if err := validator.Validate(params); err != nil {
		return false, sharedmodel.WrapErr("validate order for review", err)
	}

	return b.storage.Querier().ValidateOrderForReview(ctx, orderdb.ValidateOrderForReviewParams{
		OrderID: params.OrderID,
		BuyerID: params.AccountID,
		SkuIds:  params.SkuIDs,
	})
}

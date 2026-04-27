package orderbiz

import (
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/guregu/null/v6"
)

// ListSellerPendingItems returns paginated pending items for the seller.
func (b *OrderHandler) ListSellerPendingItems(
	ctx restate.Context,
	params ListSellerPendingItemsParams,
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	type incomingResult struct {
		Items []orderdb.OrderItem `json:"items"`
		Total int64               `json:"total"`
	}

	dbResult, err := restate.Run(ctx, func(ctx restate.RunContext) (incomingResult, error) {
		items, err := b.storage.Querier().ListSellerPendingItems(ctx, params.SellerID)
		if err != nil {
			return incomingResult{}, err
		}

		total, err := b.storage.Querier().CountSellerPendingItems(ctx, params.SellerID)
		if err != nil {
			return incomingResult{}, err
		}

		return incomingResult{Items: items, Total: total}, nil
	})
	if err != nil {
		return zero, err
	}

	enriched, err := b.enrichItems(dbResult.Items)
	if err != nil {
		return zero, err
	}

	var totalVal null.Int64
	totalVal.SetValid(dbResult.Total)

	return sharedmodel.PaginateResult[ordermodel.OrderItem]{
		PageParams: params.PaginationParams,
		Total:      totalVal,
		Data:       enriched,
	}, nil
}

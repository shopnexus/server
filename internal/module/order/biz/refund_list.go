package orderbiz

import (
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// ListBuyerRefunds returns paginated refunds owned by the requesting buyer.
func (b *OrderHandler) ListBuyerRefunds(
	ctx restate.Context,
	params ListBuyerRefundsParams,
) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	pagination := params.PaginationParams.Constrain()

	rows, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderRefund, error) {
		return b.storage.Querier().ListBuyerRefunds(ctx, orderdb.ListBuyerRefundsParams{
			AccountID:   params.BuyerID,
			OffsetCount: pagination.Offset().Int32,
			LimitCount:  pagination.Limit.Int32,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list buyer refunds", err)
	}

	data := make([]ordermodel.Refund, 0, len(rows))
	for _, r := range rows {
		data = append(data, mapRefund(r))
	}
	return sharedmodel.PaginateResult[ordermodel.Refund]{
		PageParams: pagination,
		Data:       data,
	}, nil
}

// ListSellerRefunds returns paginated refunds raised against items the
// requesting seller fulfilled. The list is the seller's pending-action queue.
func (b *OrderHandler) ListSellerRefunds(
	ctx restate.Context,
	params ListSellerRefundsParams,
) (sharedmodel.PaginateResult[ordermodel.Refund], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Refund]

	pagination := params.PaginationParams.Constrain()

	rows, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.OrderRefund, error) {
		return b.storage.Querier().ListSellerRefunds(ctx, orderdb.ListSellerRefundsParams{
			SellerID:    params.SellerID,
			OffsetCount: pagination.Offset().Int32,
			LimitCount:  pagination.Limit.Int32,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("list seller refunds", err)
	}

	data := make([]ordermodel.Refund, 0, len(rows))
	for _, r := range rows {
		data = append(data, mapRefund(r))
	}
	return sharedmodel.PaginateResult[ordermodel.Refund]{
		PageParams: pagination,
		Data:       data,
	}, nil
}

package orderbiz

import (
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

// GetBuyerOrder returns a single order by ID with all items and payment details.
// TODO: add casbin authorization — verify caller owns this order
func (b *OrderHandler) GetBuyerOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	var zero ordermodel.Order

	order, err := restate.Run(ctx, func(ctx restate.RunContext) (orderdb.OrderOrder, error) {
		return b.storage.Querier().GetOrder(ctx, uuid.NullUUID{UUID: orderID, Valid: true})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("get order", err)
	}

	hydrated, err := b.hydrateOrders(ctx, []orderdb.OrderOrder{order})
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate order", err)
	}
	if len(hydrated) == 0 {
		return zero, ordermodel.ErrOrderNotFound.Terminal()
	}

	return hydrated[0], nil
}

// GetSellerOrder returns a single order by ID (seller perspective).
// TODO: add casbin authorization — verify caller is this order's seller
func (b *OrderHandler) GetSellerOrder(ctx restate.Context, orderID uuid.UUID) (ordermodel.Order, error) {
	return b.GetBuyerOrder(ctx, orderID)
}

// ListBuyerConfirmed returns paginated orders with hydrated items, payments, and product resources.
func (b *OrderHandler) ListBuyerConfirmed(
	ctx restate.Context,
	params ListBuyerConfirmedParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list orders", err)
	}

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountBuyerOrderRow, error) {
		return b.storage.Querier().ListCountBuyerOrder(ctx, orderdb.ListCountBuyerOrderParams{
			BuyerID: params.BuyerID,
			Limit:   params.Limit,
			Offset:  params.Offset(),
		})
	})

	if err != nil {
		return zero, sharedmodel.WrapErr("db list orders", err)
	}

	var total null.Int64
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders := lo.Map(listCountOrder, func(item orderdb.ListCountBuyerOrderRow, _ int) orderdb.OrderOrder {
		return item.OrderOrder
	})
	data, err := b.hydrateOrders(ctx, orders)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate orders", err)
	}

	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       data,
	}, nil
}

// ListSellerConfirmed returns paginated orders for the seller with optional payment/order status filters.
func (b *OrderHandler) ListSellerConfirmed(
	ctx restate.Context,
	params ListSellerConfirmedParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]

	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list seller orders", err)
	}

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountSellerOrderRow, error) {
		return b.storage.Querier().ListCountSellerOrder(ctx, orderdb.ListCountSellerOrderParams{
			SellerID: params.SellerID,
			Search:   params.Search,
			Offset:   params.Offset(),
			Limit:    params.Limit,
		})
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list seller orders", err)
	}

	var total null.Int64
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders, err := b.hydrateOrders(
		ctx,
		lo.Map(listCountOrder, func(item orderdb.ListCountSellerOrderRow, _ int) orderdb.OrderOrder {
			return item.OrderOrder
		}),
	)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate seller orders", err)
	}

	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       orders,
	}, nil
}


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

// ListBuyerPendingOrders returns orders that are post-confirm but neither
// completed (payout released) nor cancelled. Includes orders awaiting
// shipment, in transit, delivered-but-not-paid-out.
func (b *OrderHandler) ListBuyerPendingOrders(
	ctx restate.Context,
	params ListBuyerPendingOrdersParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	return b.listBuyerOrders(ctx, params.PaginationParams, params.BuyerID, func(rctx restate.RunContext, p orderListPage) ([]orderdb.OrderOrder, int64, error) {
		rows, err := b.storage.Querier().ListBuyerPendingOrders(rctx, orderdb.ListBuyerPendingOrdersParams{
			BuyerID: p.BuyerID,
			Limit:   p.Limit,
			Offset:  p.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		orders := lo.Map(rows, func(r orderdb.ListBuyerPendingOrdersRow, _ int) orderdb.OrderOrder { return r.OrderOrder })
		var total int64
		if len(rows) > 0 {
			total = rows[0].TotalCount
		}
		return orders, total, nil
	})
}

// ListBuyerCompletedOrders returns orders whose seller payout has been
// released (escrow done). Delivered-but-not-paid-out orders stay Pending.
func (b *OrderHandler) ListBuyerCompletedOrders(
	ctx restate.Context,
	params ListBuyerCompletedOrdersParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	return b.listBuyerOrders(ctx, params.PaginationParams, params.BuyerID, func(rctx restate.RunContext, p orderListPage) ([]orderdb.OrderOrder, int64, error) {
		rows, err := b.storage.Querier().ListBuyerCompletedOrders(rctx, orderdb.ListBuyerCompletedOrdersParams{
			BuyerID: p.BuyerID,
			Limit:   p.Limit,
			Offset:  p.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		orders := lo.Map(rows, func(r orderdb.ListBuyerCompletedOrdersRow, _ int) orderdb.OrderOrder { return r.OrderOrder })
		var total int64
		if len(rows) > 0 {
			total = rows[0].TotalCount
		}
		return orders, total, nil
	})
}

// ListBuyerCancelledOrders returns orders where any of confirm/transport/payout
// is in a Failed or Cancelled state.
func (b *OrderHandler) ListBuyerCancelledOrders(
	ctx restate.Context,
	params ListBuyerCancelledOrdersParams,
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	return b.listBuyerOrders(ctx, params.PaginationParams, params.BuyerID, func(rctx restate.RunContext, p orderListPage) ([]orderdb.OrderOrder, int64, error) {
		rows, err := b.storage.Querier().ListBuyerCancelledOrders(rctx, orderdb.ListBuyerCancelledOrdersParams{
			BuyerID: p.BuyerID,
			Limit:   p.Limit,
			Offset:  p.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		orders := lo.Map(rows, func(r orderdb.ListBuyerCancelledOrdersRow, _ int) orderdb.OrderOrder { return r.OrderOrder })
		var total int64
		if len(rows) > 0 {
			total = rows[0].TotalCount
		}
		return orders, total, nil
	})
}

// ListBuyerCancelledItems returns pre-confirm items that died before becoming
// orders: failed/cancelled checkout sessions, or individually-refunded items
// from a Success session (date_cancelled set).
func (b *OrderHandler) ListBuyerCancelledItems(
	ctx restate.Context,
	params ListBuyerCancelledItemsParams,
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]
	if err := validator.Validate(params); err != nil {
		return zero, sharedmodel.WrapErr("validate list cancelled items", err)
	}
	return b.listBuyerItems(ctx, params.PaginationParams, params.AccountID,
		func(rctx restate.RunContext, accountID uuid.UUID) ([]orderdb.OrderItem, int64, error) {
			items, err := b.storage.Querier().ListBuyerCancelledItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			total, err := b.storage.Querier().CountBuyerCancelledItems(rctx, accountID)
			if err != nil {
				return nil, 0, err
			}
			return items, total, nil
		})
}

// orderListPage carries the per-page args into the per-query closure.
type orderListPage struct {
	BuyerID uuid.UUID
	Limit   null.Int32
	Offset  null.Int32
}

// listBuyerOrders is the shared backbone for the three order-list endpoints:
// validate -> run query in restate.Run -> hydrate -> wrap in PaginateResult.
func (b *OrderHandler) listBuyerOrders(
	ctx restate.Context,
	pagination sharedmodel.PaginationParams,
	buyerID uuid.UUID,
	fetch func(restate.RunContext, orderListPage) ([]orderdb.OrderOrder, int64, error),
) (sharedmodel.PaginateResult[ordermodel.Order], error) {
	var zero sharedmodel.PaginateResult[ordermodel.Order]
	if err := validator.Validate(struct {
		BuyerID uuid.UUID `validate:"required"`
	}{BuyerID: buyerID}); err != nil {
		return zero, sharedmodel.WrapErr("validate list orders", err)
	}

	type queryResult struct {
		Orders []orderdb.OrderOrder `json:"orders"`
		Total  int64                `json:"total"`
	}
	res, err := restate.Run(ctx, func(rctx restate.RunContext) (queryResult, error) {
		orders, total, err := fetch(rctx, orderListPage{
			BuyerID: buyerID,
			Limit:   pagination.Limit,
			Offset:  pagination.Offset(),
		})
		if err != nil {
			return queryResult{}, err
		}
		return queryResult{Orders: orders, Total: total}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list orders", err)
	}

	data, err := b.hydrateOrders(ctx, res.Orders)
	if err != nil {
		return zero, sharedmodel.WrapErr("hydrate orders", err)
	}

	var total null.Int64
	total.SetValid(res.Total)
	return sharedmodel.PaginateResult[ordermodel.Order]{
		PageParams: pagination,
		Total:      total,
		Data:       data,
	}, nil
}

// listBuyerItems is the shared backbone for buyer item-list endpoints.
// Mirrors the existing ListBuyerPendingItems shape including session attach.
func (b *OrderHandler) listBuyerItems(
	ctx restate.Context,
	pagination sharedmodel.PaginationParams,
	accountID uuid.UUID,
	fetch func(restate.RunContext, uuid.UUID) ([]orderdb.OrderItem, int64, error),
) (sharedmodel.PaginateResult[ordermodel.OrderItem], error) {
	var zero sharedmodel.PaginateResult[ordermodel.OrderItem]

	type pageResult struct {
		Items []orderdb.OrderItem `json:"items"`
		Total int64               `json:"total"`
	}
	res, err := restate.Run(ctx, func(rctx restate.RunContext) (pageResult, error) {
		items, total, err := fetch(rctx, accountID)
		if err != nil {
			return pageResult{}, err
		}
		return pageResult{Items: items, Total: total}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("db list items", err)
	}

	enriched, err := b.enrichItems(res.Items)
	if err != nil {
		return zero, sharedmodel.WrapErr("enrich items", err)
	}

	if len(enriched) > 0 {
		sessionIDs := lo.Uniq(lo.Map(enriched, func(it ordermodel.OrderItem, _ int) uuid.UUID { return it.PaymentSessionID }))
		var sessions []orderdb.OrderPaymentSession
		sessions, err = restate.Run(ctx, func(rctx restate.RunContext) ([]orderdb.OrderPaymentSession, error) {
			return b.storage.Querier().ListPaymentSession(rctx, orderdb.ListPaymentSessionParams{ID: sessionIDs})
		})
		if err != nil {
			return zero, sharedmodel.WrapErr("db fetch payment sessions", err)
		}
		sessionMap := lo.KeyBy(sessions, func(s orderdb.OrderPaymentSession) uuid.UUID { return s.ID })
		for i := range enriched {
			if s, ok := sessionMap[enriched[i].PaymentSessionID]; ok {
				mapped := mapPaymentSession(s)
				enriched[i].PaymentSession = &mapped
			}
		}
	}

	var totalVal null.Int64
	totalVal.SetValid(res.Total)
	return sharedmodel.PaginateResult[ordermodel.OrderItem]{
		PageParams: pagination,
		Total:      totalVal,
		Data:       enriched,
	}, nil
}

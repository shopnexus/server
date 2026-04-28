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

func (b *OrderHandler) hydrateOrders(ctx restate.Context, orders []orderdb.OrderOrder) ([]ordermodel.Order, error) {
	if len(orders) == 0 {
		return []ordermodel.Order{}, nil
	}

	orderIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) uuid.UUID { return o.ID })

	// Collect transport IDs
	transportIDs := lo.Uniq(lo.Map(orders, func(o orderdb.OrderOrder, _ int) int64 { return o.TransportID }))

	// Fetch order items and transports from DB inside Run
	type dbResults struct {
		OrderItems []orderdb.OrderItem      `json:"order_items"`
		Transports []orderdb.OrderTransport `json:"transports"`
	}
	dbData, err := restate.Run(ctx, func(ctx restate.RunContext) (dbResults, error) {
		orderItems, err := b.storage.Querier().ListItem(ctx, orderdb.ListItemParams{
			OrderID: lo.Map(orderIDs, func(id uuid.UUID, _ int) uuid.NullUUID {
				return uuid.NullUUID{UUID: id, Valid: true}
			}),
		})
		if err != nil {
			return dbResults{}, err
		}

		transports, err := b.storage.Querier().ListTransport(ctx, orderdb.ListTransportParams{
			ID: transportIDs,
		})
		if err != nil {
			return dbResults{}, err
		}

		return dbResults{OrderItems: orderItems, Transports: transports}, nil
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db fetch order data", err)
	}

	// Enrich all items in one batch (single ListProductSku + GetResources call)
	allEnriched, err := b.enrichItems(dbData.OrderItems)
	if err != nil {
		return nil, sharedmodel.WrapErr("enrich order items", err)
	}

	// Group enriched items by order_id
	enrichedItemsMap := make(map[uuid.UUID][]ordermodel.OrderItem)
	for _, item := range allEnriched {
		if item.OrderID.Valid {
			enrichedItemsMap[item.OrderID.UUID] = append(enrichedItemsMap[item.OrderID.UUID], item)
		}
	}

	transportMap := lo.KeyBy(dbData.Transports, func(t orderdb.OrderTransport) int64 { return t.ID })

	result := make([]ordermodel.Order, 0, len(orders))
	for _, o := range orders {
		base := mapOrder(o)

		if t, ok := transportMap[o.TransportID]; ok {
			tr := mapTransport(t)
			base.Transport = &tr
		}
		base.Items = enrichedItemsMap[o.ID]

		// Load the confirm-fee session, payout session (if any), and total amount.
		type orderEnrich struct {
			ConfirmSession orderdb.OrderPaymentSession  `json:"confirm_session"`
			PayoutSession  *orderdb.OrderPaymentSession `json:"payout_session,omitempty"`
			TotalAmount    int64                        `json:"total_amount"`
		}
		orderID := o.ID
		confirmSessionID := o.ConfirmSessionID
		enriched, err := restate.Run(ctx, func(ctx restate.RunContext) (orderEnrich, error) {
			confirmSession, err := b.storage.Querier().GetPaymentSession(ctx, uuid.NullUUID{UUID: confirmSessionID, Valid: true})
			if err != nil {
				return orderEnrich{}, sharedmodel.WrapErr("get confirm session", err)
			}
			res := orderEnrich{ConfirmSession: confirmSession}
			if payoutSession, perr := b.storage.Querier().GetPendingPayoutSessionForOrder(ctx, orderID); perr == nil {
				res.PayoutSession = &payoutSession
			}
			total, err := b.storage.Querier().SumTotalAmountByOrder(ctx, uuid.NullUUID{UUID: orderID, Valid: true})
			if err != nil {
				return orderEnrich{}, sharedmodel.WrapErr("sum paid amount by order", err)
			}
			res.TotalAmount = total
			return res, nil
		})
		if err != nil {
			return nil, sharedmodel.WrapErr("enrich order sessions", err)
		}

		base.TotalAmount = enriched.TotalAmount
		confirmMapped := mapPaymentSession(enriched.ConfirmSession)
		base.ConfirmSession = &confirmMapped
		if enriched.PayoutSession != nil {
			payoutMapped := mapPaymentSession(*enriched.PayoutSession)
			base.PayoutSession = &payoutMapped
		}

		result = append(result, base)
	}

	return result, nil
}

// mapTransport maps a DB OrderTransport row to the model type.
func mapTransport(t orderdb.OrderTransport) ordermodel.Transport {
	return ordermodel.Transport{
		ID:          t.ID,
		Option:      t.Option,
		Status:      t.Status,
		Data:        t.Data,
		DateCreated: t.DateCreated,
	}
}

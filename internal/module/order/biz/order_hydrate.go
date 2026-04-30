package orderbiz

import (
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

// hydrateOrders fans out one DB pull for items + transports per page, enriches
// items with product/resource data, then enriches each order with its confirm +
// payout session and total amount. The payout session loads regardless of
// status so the FE can render "Funds released" once it reaches Success.
func (b *OrderHandler) hydrateOrders(ctx restate.Context, orders []orderdb.OrderOrder) ([]ordermodel.Order, error) {
	if len(orders) == 0 {
		return []ordermodel.Order{}, nil
	}

	orderIDs := lo.Map(orders, func(o orderdb.OrderOrder, _ int) uuid.UUID { return o.ID })
	transportIDs := lo.Uniq(lo.Map(orders, func(o orderdb.OrderOrder, _ int) int64 { return o.TransportID }))

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
		transports, err := b.storage.Querier().ListTransport(ctx, orderdb.ListTransportParams{ID: transportIDs})
		if err != nil {
			return dbResults{}, err
		}
		return dbResults{OrderItems: orderItems, Transports: transports}, nil
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("db fetch order data", err)
	}

	allEnriched, err := b.enrichItems(dbData.OrderItems)
	if err != nil {
		return nil, sharedmodel.WrapErr("enrich order items", err)
	}

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
			if payoutSession, perr := b.storage.Querier().GetPayoutSessionForOrder(ctx, orderID); perr == nil {
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

func mapTransport(t orderdb.OrderTransport) ordermodel.Transport {
	return ordermodel.Transport{
		ID:          t.ID,
		OptionID:    t.Option,
		Status:      t.Status,
		Data:        t.Data,
		DateCreated: t.DateCreated,
	}
}

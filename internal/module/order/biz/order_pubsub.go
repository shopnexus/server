package orderbiz

import (
	"context"
	"errors"

	"shopnexus-server/internal/infras/pubsub"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"

	"github.com/google/uuid"
)

func (b *OrderBiz) SetupPubsub() error {
	return errors.Join(
		b.pubsub.Subscribe(ordermodel.TopicOrderCreated, pubsub.DecodeWrap(b.OrderCreated)),
		b.pubsub.Subscribe(ordermodel.TopicOrderPaid, pubsub.DecodeWrap(b.OrderPaid)),
	)
}

type OrderCreatedParams = struct {
	OrderID uuid.UUID
}

func (b *OrderBiz) OrderCreated(ctx context.Context, params OrderCreatedParams) error {
	//
	return nil
}

type OrderPaidParams = struct {
	OrderID uuid.UUID
}

func (b *OrderBiz) OrderPaid(ctx context.Context, params OrderPaidParams) error {
	order, err := b.storage.Querier().GetOrder(ctx, orderdb.GetOrderParams{
		ID: uuid.NullUUID{UUID: params.OrderID, Valid: true},
	})
	if err != nil {
		return err
	}

	_, err = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
		ID:     order.PaymentID,
		Status: orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusSuccess, Valid: true},
	})
	return err
}

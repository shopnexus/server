package orderbiz

import (
	"context"
	"errors"

	"shopnexus-remastered/internal/infras/pubsub"
	orderdb "shopnexus-remastered/internal/module/order/db"
	ordermodel "shopnexus-remastered/internal/module/order/model"

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
	// ! should not use txStorage here
	// txStorage, err := s.storage.BeginTx(ctx)
	// if err != nil {
	// 	return err
	// }
	// defer txStorage.Rollback(ctx)

	// Get order to find payment ID
	order, err := b.storage.Querier().GetOrder(ctx, orderdb.GetOrderParams{
		ID: uuid.NullUUID{UUID: params.OrderID, Valid: true},
	})
	if err != nil {
		return err
	}

	// Update the payment status to success
	_, err = b.storage.Querier().UpdatePayment(ctx, orderdb.UpdatePaymentParams{
		ID:     order.PaymentID,
		Status: orderdb.NullCommonStatus{CommonStatus: orderdb.CommonStatusSuccess, Valid: true},
	})
	if err != nil {
		return err
	}

	return nil
}

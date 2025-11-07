package orderbiz

import (
	"context"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	"shopnexus-remastered/internal/utils/errutil"
)

func (b *OrderBiz) SetupPubsub() error {
	return errutil.Some(
		b.pubsub.Subscribe(ordermodel.TopicOrderCreated, pubsub.DecodeWrap(b.OrderCreated)),
		b.pubsub.Subscribe(ordermodel.TopicOrderPaid, pubsub.DecodeWrap(b.OrderPaid)),
	)
}

type OrderCreatedParams = struct {
	OrderID int64
}

func (b *OrderBiz) OrderCreated(ctx context.Context, params OrderCreatedParams) error {
	//
	return nil
}

type OrderPaidParams = struct {
	OrderID int64
}

func (b *OrderBiz) OrderPaid(ctx context.Context, params OrderPaidParams) error {
	// ! should not use txStorage here
	// txStorage, err := s.storage.BeginTx(ctx)
	// if err != nil {
	// 	return err
	// }
	// defer txStorage.Rollback(ctx)

	// Update the order status to success
	_, err := b.storage.UpdateOrderBase(ctx, db.UpdateOrderBaseParams{
		ID:            params.OrderID,
		PaymentStatus: db.NullCommonStatus{CommonStatus: db.CommonStatusSuccess, Valid: true},
	})
	if err != nil {
		return err
	}

	return nil
}

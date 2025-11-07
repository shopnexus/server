package orderbiz

import (
	"context"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	"shopnexus-remastered/internal/utils/errutil"
)

func (s *OrderBiz) SetupPubsub() error {
	return errutil.Some(
		s.pubsub.Subscribe(ordermodel.TopicOrderCreated, pubsub.DecodeWrap(s.OrderCreated)),
		s.pubsub.Subscribe(ordermodel.TopicOrderPaid, pubsub.DecodeWrap(s.OrderPaid)),
	)
}

type OrderCreatedParams = struct {
	OrderID int64
}

func (s *OrderBiz) OrderCreated(ctx context.Context, params OrderCreatedParams) error {
	//
	return nil
}

type OrderPaidParams = struct {
	OrderID int64
}

func (s *OrderBiz) OrderPaid(ctx context.Context, params OrderPaidParams) error {
	// ! should not use txStorage here
	// txStorage, err := s.storage.BeginTx(ctx)
	// if err != nil {
	// 	return err
	// }
	// defer txStorage.Rollback(ctx)

	// Update the order status to success
	_, err := s.storage.UpdateOrderBase(ctx, db.UpdateOrderBaseParams{
		ID:            params.OrderID,
		PaymentStatus: db.NullSharedStatus{SharedStatus: db.SharedStatusSuccess, Valid: true},
	})
	if err != nil {
		return err
	}

	return nil
}

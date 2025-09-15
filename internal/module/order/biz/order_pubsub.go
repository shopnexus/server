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
	// code here

	return nil
}

type OrderPaidParams = struct {
	OrderID int64
}

func (s *OrderBiz) OrderPaid(ctx context.Context, params OrderPaidParams) error {
	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	// Update the order status to success
	order, err := txStorage.UpdateOrderBase(ctx, db.UpdateOrderBaseParams{
		ID:     params.OrderID,
		Status: db.NullSharedStatus{SharedStatus: db.SharedStatusSuccess, Valid: true},
	})
	if err != nil {
		return err
	}

	_ = order // use order if needed

	if err = txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}

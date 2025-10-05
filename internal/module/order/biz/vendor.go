package orderbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
	"time"
)

type ConfirmOrderParams struct {
	Account authmodel.AuthenticatedAccount
	OrderID int64 `validate:"required,min=1"`
}

func (s *OrderBiz) ConfirmOrder(ctx context.Context, params ConfirmOrderParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	txStorage, err := s.storage.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer txStorage.Rollback(ctx)

	order, err := txStorage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(params.OrderID))
	if err != nil {
		return err
	}

	if order.Status != db.SharedStatusPending {
		return fmt.Errorf("only pending orders can be confirmed")
	}

	if _, err := txStorage.UpdateOrderBase(ctx, db.UpdateOrderBaseParams{
		ID:            params.OrderID,
		ConfirmedByID: pgutil.Int64ToPgInt8(params.Account.ID),
		Status:        db.NullSharedStatus{SharedStatus: db.SharedStatusProcessing, Valid: true},
		DateUpdated:   pgutil.TimeToPgTimestamptz(time.Now()),
	}); err != nil {
		return err
	}

	// TODO: push confirmed event for shipment creation

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	return nil
}

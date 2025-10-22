package orderbiz

import (
	"context"
	"fmt"

	"shopnexus-remastered/internal/client/shipment"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"

	"github.com/guregu/null/v6"
)

type ListVendorOrderParams struct {
	Account authmodel.AuthenticatedAccount
	sharedmodel.PaginationParams
}

func (s *OrderBiz) ListVendorOrder(ctx context.Context, params ListVendorOrderParams) (sharedmodel.PaginateResult[db.OrderItem], error) {
	var zero sharedmodel.PaginateResult[db.OrderItem]

	total, err := s.storage.CountOrderItem(ctx, db.CountOrderItemParams{
		VendorID: []int64{params.Account.ID},
	})
	if err != nil {
		return zero, err
	}

	orders, err := s.storage.ListOrderItem(ctx, db.ListOrderItemParams{
		Limit:    pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:   pgutil.Int32ToPgInt4(params.Offset()),
		VendorID: []int64{params.Account.ID},
	})
	if err != nil {
		return zero, err
	}

	return sharedmodel.PaginateResult[db.OrderItem]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       orders,
	}, nil
}

// ConfirmOrderParams represents the parameters required to confirm an order by SKU (not the whole order).
type ConfirmOrderParams struct {
	Account     authmodel.AuthenticatedAccount
	OrderItemID int64 `validate:"required,min=1"` // Confirmed SKU

	FromAddress null.String `validate:"omitnil,min=5,max=500"` // Optional updated from address (in case vendor wants to change warehouse address)
	WeightGrams int32       `validate:"required,min=1"`        // Revalidated weight, dimensions
	LengthCM    int32       `validate:"required,min=1"`
	WidthCM     int32       `validate:"required,min=1"`
	HeightCM    int32       `validate:"required,min=1"`
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

	orderItem, err := txStorage.GetOrderItem(ctx, pgutil.Int64ToPgInt8(params.OrderItemID))
	if err != nil {
		return err
	}

	if orderItem.Status != db.SharedStatusPending {
		return fmt.Errorf("only pending order items can be confirmed")
	}

	orderItem, err = txStorage.UpdateOrderItem(ctx, db.UpdateOrderItemParams{
		ID:            params.OrderItemID,
		ConfirmedByID: pgutil.Int64ToPgInt8(params.Account.ID),
		Status:        db.NullSharedStatus{SharedStatus: db.SharedStatusProcessing, Valid: true},
	})
	if err != nil {
		return err
	}

	dbShipment, err := txStorage.GetOrderShipment(ctx, pgutil.Int64ToPgInt8(orderItem.ShipmentID))
	if err != nil {
		return err
	}

	shipmentClient, ok := s.shipmentMap[dbShipment.Option]
	if !ok {
		return fmt.Errorf("unknown shipment option: %s", dbShipment.Option)
	}

	var fromAddress string
	if params.FromAddress.Valid {
		fromAddress = params.FromAddress.String
	} else {
		fromAddress = dbShipment.FromAddress
	}

	ship, err := shipmentClient.Create(ctx, shipment.CreateParams{
		FromAddress: fromAddress,
		ToAddress:   dbShipment.ToAddress,
		WeightGrams: dbShipment.WeightGrams,
		LengthCM:    dbShipment.LengthCm,
		WidthCM:     dbShipment.WidthCm,
		HeightCM:    dbShipment.HeightCm,
	})
	if err != nil {
		return fmt.Errorf("failed to create shipment: %w", err)
	}

	_, err = txStorage.UpdateOrderShipment(ctx, db.UpdateOrderShipmentParams{
		ID:           orderItem.ShipmentID,
		TrackingCode: pgutil.StringToPgText(ship.ID),
		Status:       db.NullOrderShipmentStatus{OrderShipmentStatus: db.OrderShipmentStatusLabelCreated, Valid: true},
		LabelUrl:     pgutil.StringToPgText("https://example.com/label.pdf"), // TODO: get real label URL from shipment client
		Cost:         pgutil.Int64ToPgInt8(dbShipment.Cost),
		NewCost:      pgutil.Int64ToPgInt8(ship.Costs.Int64()),
		DateEta:      pgutil.TimeToPgTimestamptz(ship.ETA),
		FromAddress:  pgutil.StringToPgText(fromAddress),
		WeightGrams:  pgutil.Int32ToPgInt4(params.WeightGrams),
		LengthCm:     pgutil.Int32ToPgInt4(params.LengthCM),
		WidthCm:      pgutil.Int32ToPgInt4(params.WidthCM),
		HeightCm:     pgutil.Int32ToPgInt4(params.HeightCM),
	})
	if err != nil {
		return err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	order, err := s.storage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(orderItem.OrderID))
	if err != nil {
		return err
	}
	if err := s.pubsub.Publish(ordermodel.TopicOrderConfirmed, order); err != nil {
		return err
	}

	return nil
}

package orderbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/internal/client/shipment"
	"shopnexus-remastered/internal/db"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
)

// ConfirmOrderParams represents the parameters required to confirm an order by SKU (not the whole order).
type ConfirmOrderParams struct {
	Account     authmodel.AuthenticatedAccount
	SkuID       int64  `validate:"required,min=1"` // Confirmed SKU
	FromAddress string `validate:"required"`
	WeightGrams int64  `validate:"required,min=1"` // in grams
	Dimensions  shipment.Dimensions
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

	orderItem, err := txStorage.GetOrderItem(ctx, pgutil.Int64ToPgInt8(params.SkuID))
	if err != nil {
		return err
	}

	orderItem, err = txStorage.UpdateOrderItem(ctx, db.UpdateOrderItemParams{
		ID:            params.SkuID,
		ConfirmedByID: pgutil.Int64ToPgInt8(params.Account.ID),
		Status:        db.NullSharedStatus{SharedStatus: db.SharedStatusProcessing, Valid: true},
	})
	if err != nil {
		return err
	}

	// Create shipment
	shipmentClient, ok := s.shipmentMap[orderItem.ShipmentProvider]
	if !ok {
		return fmt.Errorf("unsupported shipment provider: %s", orderItem.ShipmentProvider)
	}

	order, err := txStorage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(orderItem.OrderID))
	if err != nil {
		return err
	}

	shipment, err := shipmentClient.CreateShipment(ctx, shipment.CreateShipmentParams{
		OrderID:     fmt.Sprintf("%d", orderItem.OrderID),
		FromAddress: params.FromAddress,
		ToAddress:   order.Address,
		WeightGrams: params.WeightGrams,
		Dimensions:  params.Dimensions,
		Service:     "express",
	})
	if err != nil {
		return err
	}

	_, err = txStorage.CreateOrderShipment(ctx, db.CreateOrderShipmentParams{
		Provider:     orderItem.ShipmentProvider,
		TrackingCode: pgutil.StringToPgText(shipment.TrackingID),
		Status:       db.OrderShipmentStatusPending,
		LabelUrl:     pgutil.StringToPgText("https://example.com/label.pdf"),
		Cost:         shipment.CostCents,
		DateEta:      pgutil.TimeToPgTimestamptz(shipment.ETA),
	})
	if err != nil {
		return err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return err
	}

	if err := s.pubsub.Publish(ordermodel.TopicOrderConfirmed, order); err != nil {
		return err
	}

	return nil
}

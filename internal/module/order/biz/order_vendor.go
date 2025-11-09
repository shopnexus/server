package orderbiz

import (
	"context"
	"encoding/json"
	"fmt"

	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/infras/shipment"
	authmodel "shopnexus-remastered/internal/module/auth/model"
	commonmodel "shopnexus-remastered/internal/module/common/model"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	"shopnexus-remastered/internal/module/shared/pgsqlc"
	"shopnexus-remastered/internal/module/shared/pgutil"
	"shopnexus-remastered/internal/module/shared/validator"

	"github.com/guregu/null/v6"
)

type ListVendorOrderParams struct {
	Account authmodel.AuthenticatedAccount
	commonmodel.PaginationParams
}

func (b *OrderBiz) ListVendorOrder(ctx context.Context, params ListVendorOrderParams) (commonmodel.PaginateResult[db.ListVendorOrderItemRow], error) {
	var zero commonmodel.PaginateResult[db.ListVendorOrderItemRow]

	total, err := b.storage.CountOrderItem(ctx, db.CountOrderItemParams{
		VendorID: []int64{params.Account.ID},
	})
	if err != nil {
		return zero, err
	}

	orders, err := b.storage.ListVendorOrderItem(ctx, db.ListVendorOrderItemParams{
		Limit:    pgutil.Int32ToPgInt4(params.GetLimit()),
		Offset:   pgutil.Int32ToPgInt4(params.Offset()),
		VendorID: []int64{params.Account.ID},
	})
	if err != nil {
		return zero, err
	}

	return commonmodel.PaginateResult[db.ListVendorOrderItemRow]{
		PageParams: params.PaginationParams,
		Total:      null.IntFrom(total),
		Data:       orders,
	}, nil
}

// ConfirmOrderParams represents the parameters required to confirm an order by SKU (not the whole order).
type ConfirmOrderParams struct {
	Storage     pgsqlc.Storage
	Account     authmodel.AuthenticatedAccount
	OrderItemID int64 `validate:"required,min=1"` // Confirmed SKU

	FromAddress null.String     `validate:"omitnil,min=5,max=500"` // Optional updated from address (in case vendor wants to change warehouse address)
	Specs       json.RawMessage `validate:"required"`              // JSON object with weight and dimensions
}

func (b *OrderBiz) ConfirmOrder(ctx context.Context, params ConfirmOrderParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	var orderItem db.OrderItem

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage pgsqlc.Storage) error {
		var err error
		orderItem, err = txStorage.GetOrderItem(ctx, pgutil.Int64ToPgInt8(params.OrderItemID))
		if err != nil {
			return err
		}

		if orderItem.Status != db.CommonStatusPending {
			return fmt.Errorf("only pending order items can be confirmed")
		}

		orderItem, err = txStorage.UpdateOrderItem(ctx, db.UpdateOrderItemParams{
			ID:            params.OrderItemID,
			ConfirmedByID: pgutil.Int64ToPgInt8(params.Account.ID),
			Status:        db.NullCommonStatus{CommonStatus: db.CommonStatusProcessing, Valid: true},
		})
		if err != nil {
			return err
		}

		dbShipment, err := txStorage.GetOrderShipment(ctx, pgutil.Int64ToPgInt8(orderItem.ShipmentID))
		if err != nil {
			return err
		}

		shipmentClient, ok := b.shipmentMap[dbShipment.Option]
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
			// TODO: use Specs
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to confirm order: %w", err)
	}

	order, err := b.storage.GetOrderBase(ctx, pgutil.Int64ToPgInt8(orderItem.OrderID))
	if err != nil {
		return err
	}
	if err := b.pubsub.Publish(ordermodel.TopicOrderConfirmed, order); err != nil {
		return err
	}

	return nil
}

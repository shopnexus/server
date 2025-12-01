package orderbiz

import (
	"context"
	"encoding/json"
	"fmt"

	"shopnexus-remastered/internal/infras/shipment"
	accountmodel "shopnexus-remastered/internal/module/account/model"
	orderdb "shopnexus-remastered/internal/module/order/db/sqlc"
	ordermodel "shopnexus-remastered/internal/module/order/model"
	commonmodel "shopnexus-remastered/internal/shared/model"
	"shopnexus-remastered/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListVendorOrderParams struct {
	Account accountmodel.AuthenticatedAccount
	commonmodel.PaginationParams
}

func (b *OrderBiz) ListVendorOrder(ctx context.Context, params ListVendorOrderParams) (commonmodel.PaginateResult[ordermodel.Order], error) {
	var zero commonmodel.PaginateResult[ordermodel.Order]
	listCountOrder, err := b.storage.Querier().ListCountVendorOrder(ctx, orderdb.ListCountVendorOrderParams{
		Limit:    params.Limit,
		Offset:   params.Offset(),
		VendorID: []uuid.UUID{params.Account.ID},
	})
	if err != nil {
		return zero, err
	}

	var total null.Int
	if len(listCountOrder) > 0 {
		total.SetValid(listCountOrder[0].TotalCount)
	}

	orders, err := b.hydrateOrders(ctx, lo.Map(listCountOrder, func(item orderdb.ListCountVendorOrderRow, _ int) orderdb.OrderOrder {
		return item.OrderOrder
	}))
	if err != nil {
		return zero, err
	}

	return commonmodel.PaginateResult[ordermodel.Order]{
		PageParams: params.PaginationParams,
		Total:      total,
		Data:       orders,
	}, nil
}

// ConfirmOrderParams represents the parameters required to confirm an order by SKU (not the whole order).
type ConfirmOrderParams struct {
	Storage OrderStorage
	Account accountmodel.AuthenticatedAccount
	OrderID uuid.UUID `validate:"required"`

	// Update the shipment if needed
	FromAddress null.String     `validate:"omitnil,min=5,max=500"` // Optional updated from address (in case vendor wants to change warehouse address)
	Package     json.RawMessage `validate:"omitempty"`             // JSON object with weight and dimensions
}

func (b *OrderBiz) ConfirmOrder(ctx context.Context, params ConfirmOrderParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	order, err := b.GetOrder(ctx, params.OrderID)
	if err != nil {
		return err
	}
	if order.Payment.Status != orderdb.CommonStatusSuccess || order.Status != orderdb.CommonStatusPending {
		return fmt.Errorf("order is not in a confirmable state (payment status: %s, order status: %s)", order.Payment.Status, order.Status)
	}

	if err := b.storage.WithTx(ctx, params.Storage, func(txStorage OrderStorage) error {
		_, err := txStorage.Querier().UpdateOrder(ctx, orderdb.UpdateOrderParams{
			ID:            order.ID,
			ConfirmedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			Status:        orderdb.NullCommonStatus{CommonStatus: orderdb.CommonStatusProcessing, Valid: true},
		})

		dbShipment, err := txStorage.Querier().GetShipment(ctx, uuid.NullUUID{UUID: order.ShipmentID, Valid: true})
		if err != nil {
			return err
		}

		shipmentClient, ok := b.shipmentMap[dbShipment.Option]
		if !ok {
			return fmt.Errorf("unknown shipment option: %s", dbShipment.Option)
		}

		var (
			needUpdate     bool
			packageDetails shipment.PackageDetails
			fromAddress    string
		)

		// Check if we need to update shipment details
		if params.FromAddress.Valid {
			needUpdate = true
			fromAddress = params.FromAddress.String
		}

		// Check if we need to update package details
		if params.Package != nil {
			if err := validator.Unmarshal(params.Package, &packageDetails); err != nil {
				return fmt.Errorf("failed to unmarshal package: %w", err)
			}
			needUpdate = true
		}

		if needUpdate {
			ship, err := shipmentClient.Create(ctx, shipment.CreateParams{
				FromAddress: fromAddress,
				ToAddress:   dbShipment.ToAddress,
				Package:     packageDetails,
			})
			if err != nil {
				return fmt.Errorf("failed to create shipment: %w", err)
			}

			_, err = txStorage.Querier().UpdateShipment(ctx, orderdb.UpdateShipmentParams{
				ID:           order.ShipmentID,
				TrackingCode: null.StringFrom(ship.ID),
				Status:       orderdb.NullOrderShipmentStatus{OrderShipmentStatus: orderdb.OrderShipmentStatusLabelCreated, Valid: true},
				LabelUrl:     null.StringFrom("https://example.com/label.pdf"), // TODO: get real label URL from shipment client
				NewCost:      null.IntFrom(int64(ship.Costs)),
				DateEta:      null.TimeFrom(ship.ETA),
				FromAddress:  null.StringFrom(fromAddress),
				WeightGrams:  null.Int32From(packageDetails.WeightGrams),
				LengthCm:     null.Int32From(packageDetails.LengthCM),
				WidthCm:      null.Int32From(packageDetails.WidthCM),
				HeightCm:     null.Int32From(packageDetails.HeightCM),
			})
			if err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to confirm order: %w", err)
	}

	if err := b.pubsub.Publish(ordermodel.TopicOrderConfirmed, order); err != nil {
		return err
	}

	return nil
}

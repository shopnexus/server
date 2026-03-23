package orderbiz

import (
	"encoding/json"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"shopnexus-server/internal/infras/shipment"
	accountmodel "shopnexus-server/internal/module/account/model"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	ordermodel "shopnexus-server/internal/module/order/model"
	commonmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/samber/lo"
)

type ListVendorOrderParams struct {
	Account accountmodel.AuthenticatedAccount
	commonmodel.PaginationParams
}

func (b *OrderBiz) ListVendorOrder(ctx restate.Context, params ListVendorOrderParams) (commonmodel.PaginateResult[ordermodel.Order], error) {
	var zero commonmodel.PaginateResult[ordermodel.Order]

	listCountOrder, err := restate.Run(ctx, func(ctx restate.RunContext) ([]orderdb.ListCountVendorOrderRow, error) {
		return b.storage.Querier().ListCountVendorOrder(ctx, orderdb.ListCountVendorOrderParams{
			Limit:    params.Limit,
			Offset:   params.Offset(),
			VendorID: []uuid.UUID{params.Account.ID},
		})
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
	Account accountmodel.AuthenticatedAccount
	OrderID uuid.UUID `validate:"required"`

	// Update the shipment if needed
	FromAddress null.String     `validate:"omitnil,min=5,max=500"` // Optional updated from address (in case vendor wants to change warehouse address)
	Package     json.RawMessage `validate:"omitempty"`             // JSON object with weight and dimensions
}

func (b *OrderBiz) ConfirmOrder(ctx restate.Context, params ConfirmOrderParams) error {
	if err := validator.Validate(params); err != nil {
		return err
	}

	// GetOrder has its own Run internally
	order, err := b.GetOrder(ctx, params.OrderID)
	if err != nil {
		return err
	}
	if order.Payment.Status != orderdb.OrderStatusSuccess || order.Status != orderdb.OrderStatusPending {
		return ordermodel.ErrOrderNotConfirmable
	}

	// Update order + shipment in one durable step
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().UpdateOrder(ctx, orderdb.UpdateOrderParams{
			ID:            order.ID,
			ConfirmedByID: uuid.NullUUID{UUID: params.Account.ID, Valid: true},
			Status:        orderdb.NullOrderStatus{OrderStatus: orderdb.OrderStatusProcessing, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("update order status: %w", err)
		}

		dbShipment, err := b.storage.Querier().GetShipment(ctx, uuid.NullUUID{UUID: order.ShipmentID, Valid: true})
		if err != nil {
			return err
		}

		shipmentClient, ok := b.shipmentMap[dbShipment.Option]
		if !ok {
			return ordermodel.ErrUnknownShipmentOption.Fmt(dbShipment.Option)
		}

		var (
			needUpdate     bool
			packageDetails shipment.PackageDetails
			fromAddress    string
		)

		if params.FromAddress.Valid {
			needUpdate = true
			fromAddress = params.FromAddress.String
		}
		if params.Package != nil {
			if err := validator.Unmarshal(params.Package, &packageDetails); err != nil {
				return fmt.Errorf("unmarshal package: %w", err)
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
				return fmt.Errorf("create shipment: %w", err)
			}

			_, err = b.storage.Querier().UpdateShipment(ctx, orderdb.UpdateShipmentParams{
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
		return err
	}

	return nil
}

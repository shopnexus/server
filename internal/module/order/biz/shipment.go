package orderbiz

import (
	"context"
	"fmt"
	"shopnexus-remastered/internal/client/shipment"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/module/shared/transport/echo/validator"
	"shopnexus-remastered/internal/utils/pgutil"
)

func (b *OrderBiz) SetupShipmentProvider() error {
	b.shipmentMap = make(map[string]shipment.Client)

	// Just a fake client
	b.shipmentMap["ghtk"] = shipment.NewGTKClient("https://services.ghtklab.com", "your-api-key", "your-client-id")

	return nil
}

type CreateShipmentParams struct {
	Provider string // e.g. "ghtk", "ghn", "dhl", etc.
}

// TODO: catch event vendor confirmed order, then create shipment
func (b *OrderBiz) CreateShipment(ctx context.Context, params CreateShipmentParams) (db.OrderShipment, error) {
	var zero db.OrderShipment

	if err := validator.Validate(params); err != nil {
		return zero, err
	}

	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return zero, err
	}
	defer txStorage.Rollback(ctx)

	shipmentClient, ok := b.shipmentMap[params.Provider]
	if !ok {
		return zero, fmt.Errorf("unsupported shipment provider: %s", params.Provider)
	}

	shipment, err := shipmentClient.CreateShipment(ctx, shipment.CreateShipmentParams{
		OrderID:     "12345",
		FromAddress: "Warehouse Address",
		ToAddress:   "Customer Address",
		WeightGrams: 1500,
		Dimensions: shipment.Dimensions{
			LengthCM: 30,
			WidthCM:  20,
			HeightCM: 10,
		},
		Service: "express",
	})
	if err != nil {
		return zero, err
	}

	dbShipment, err := txStorage.CreateOrderShipment(ctx, db.CreateOrderShipmentParams{
		Provider:     params.Provider,
		TrackingCode: pgutil.StringToPgText(shipment.TrackingID),
		Status:       db.OrderShipmentStatusPending,
		LabelUrl:     pgutil.StringToPgText("https://example.com/label.pdf"),
		Cost:         shipment.CostCents,
		DateEta:      pgutil.TimeToPgTimestamptz(shipment.ETA),
	})
	if err != nil {
		return zero, err
	}

	if err := txStorage.Commit(ctx); err != nil {
		return zero, err
	}

	return dbShipment, nil
}

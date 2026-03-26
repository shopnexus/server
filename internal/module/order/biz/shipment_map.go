package orderbiz

import (
	"context"
	"log/slog"

	"shopnexus-server/internal/infras/shipment"
	"shopnexus-server/internal/infras/shipment/ghtk"
	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func (b *OrderHandler) SetupShipmentMap() error {
	var options []sharedmodel.OptionConfig
	b.shipmentMap = make(map[string]shipment.Client)

	// Setup GHTK clients
	// TODO: Load API keys from config or environment
	ghtkClients := ghtk.NewClients("https://services.ghtklab.com", "your-api-key", "your-client-id")
	for _, c := range ghtkClients {
		b.shipmentMap[c.Config().ID] = c
		options = append(options, c.Config())
	}

	// Register shipment options in background — Restate may not be ready at init time
	go func() {
		if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
			Category: "shipment",
			Configs:  options,
		}); err != nil {
			slog.Warn("register shipment options: %v", slog.Any("error", err))
		}
	}()

	return nil
}

func (b *OrderHandler) getShipmentClient(option string) (shipment.Client, error) {
	client, ok := b.shipmentMap[option]
	if !ok {
		return nil, ordermodel.ErrUnknownShipmentOption.Fmt(option).Terminal()
	}
	return client, nil
}

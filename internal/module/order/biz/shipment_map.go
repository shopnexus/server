package orderbiz

import (
	"context"

	"shopnexus-server/internal/infras/shipment"
	"shopnexus-server/internal/infras/shipment/ghtk"
	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func (b *OrderBiz) SetupShipmentMap() error {
	var options []sharedmodel.OptionConfig
	b.shipmentMap = make(map[string]shipment.Client)

	// Setup GHTK clients
	// TODO: Load API keys from config or environment
	ghtkClients := ghtk.NewClients("https://services.ghtklab.com", "your-api-key", "your-client-id")
	for _, c := range ghtkClients {
		b.shipmentMap[c.Config().ID] = c
		options = append(options, c.Config())
	}

	// TODO: should use message queue to update
	if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
		// Storage:  b.storage,
		Category: "shipment",
		Configs:  options,
	}); err != nil {
		return err
	}

	return nil
}

func (b *OrderBiz) getShipmentClient(option string) (shipment.Client, error) {
	client, ok := b.shipmentMap[option]
	if !ok {
		return nil, ordermodel.ErrUnknownShipmentOption.Fmt(option)
	}
	return client, nil
}

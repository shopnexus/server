package orderbiz

import (
	"context"
	"shopnexus-remastered/internal/client/shipment"
	"shopnexus-remastered/internal/client/shipment/ghtk"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
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

	if err := b.shared.UpdateServiceOptions(context.Background(), "shipment", options); err != nil {
		return err
	}

	return nil
}

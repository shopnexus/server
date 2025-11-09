package orderbiz

import (
	"context"

	"shopnexus-remastered/internal/infras/shipment"
	"shopnexus-remastered/internal/infras/shipment/ghtk"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonmodel "shopnexus-remastered/internal/module/common/model"
)

func (b *OrderBiz) SetupShipmentMap() error {
	var options []commonmodel.OptionConfig
	b.shipmentMap = make(map[string]shipment.Client)

	// Setup GHTK clients
	// TODO: Load API keys from config or environment
	ghtkClients := ghtk.NewClients("https://services.ghtklab.com", "your-api-key", "your-client-id")
	for _, c := range ghtkClients {
		b.shipmentMap[c.Config().ID] = c
		options = append(options, c.Config())
	}

	if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
		Storage:  b.storage,
		Category: "shipment",
		Configs:  options,
	}); err != nil {
		return err
	}

	return nil
}

package orderbiz

import (
	"shopnexus-remastered/internal/client/shipment"
)

func (b *OrderBiz) SetupShipmentProvider() error {
	b.shipmentMap = make(map[string]shipment.Client)

	// Just a fake client
	b.shipmentMap["ghtk"] = shipment.NewGTKClient("https://services.ghtklab.com", "your-api-key", "your-client-id")

	return nil
}

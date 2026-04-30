package orderecho

import (
	"shopnexus-server/internal/provider/transport"
	"shopnexus-server/internal/provider/transport/ghtk"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// newTransportClient dispatches Option to the matching provider; nil if unknown.
func newTransportClient(opt sharedmodel.Option) transport.Client {
	switch opt.Provider {
	case "ghtk":
		return ghtk.NewClient(opt)
	default:
		return nil
	}
}

package orderbiz

import (
	"context"
	"encoding/json"
	"log/slog"

	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/transport"
	"shopnexus-server/internal/provider/transport/ghtk"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// SetupTransportMap registers the transport options in the central catalog.
// Clients themselves are built on demand — nothing is cached on the handler.
func (b *OrderHandler) SetupTransportMap() error {
	configs := b.transportOptions()

	go func() {
		if err := b.common.UpsertOptions(context.Background(), commonbiz.UpsertOptionsParams{
			Category: string(sharedmodel.OptionTypeTransport),
			Configs:  configs,
		}); err != nil {
			slog.Warn("register transport options", slog.Any("error", err))
		}
	}()

	return nil
}

// transportFactory routes a transport Option to its provider-specific constructor.
func transportFactory(cfg sharedmodel.Option) transport.Client {
	switch cfg.Provider {
	case "ghtk":
		return ghtk.NewClient(cfg)
	default:
		slog.Warn("unknown transport provider", "provider", cfg.Provider, "id", cfg.ID)
		return nil
	}
}

func (b *OrderHandler) transportOptions() []sharedmodel.Option {
	var configs []sharedmodel.Option

	ghtkCfg := b.config.App.GHTK
	for _, method := range []string{ghtk.ServiceExpress, ghtk.ServiceStandard, ghtk.ServiceEconomy} {
		data, _ := json.Marshal(ghtk.Data{
			Method:   method,
			BaseURL:  ghtkCfg.BaseURL,
			APIKey:   ghtkCfg.APIKey,
			ClientID: ghtkCfg.ClientID,
			Secret:   ghtkCfg.Secret,
		})
		configs = append(configs, sharedmodel.Option{
			ID:          "ghtk_" + method,
			Type:        sharedmodel.OptionTypeTransport,
			Provider:    "ghtk",
			Name:        "Giao hàng tiết kiệm - " + method,
			Description: "Dịch vụ giao hàng nhanh của Giao hàng tiết kiệm",
			Data:        data,
		})
	}

	return configs
}

// getTransportClient builds a transport client on demand for the given option ID.
// The lookup walks the config-derived option list — no per-handler cache.
func (b *OrderHandler) getTransportClient(option string) (transport.Client, error) {
	for _, cfg := range b.transportOptions() {
		if cfg.ID == option {
			if client := transportFactory(cfg); client != nil {
				return client, nil
			}
			break
		}
	}
	return nil, ordermodel.ErrUnknownTransportOption
}

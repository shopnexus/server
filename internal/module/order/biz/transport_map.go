package orderbiz

import (
	"context"
	"log/slog"

	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/transport"
	"shopnexus-server/internal/provider/transport/ghtk"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func (b *OrderHandler) SetupTransportMap() error {
	var options []sharedmodel.OptionConfig
	b.transportMap = make(map[string]transport.Client)

	// Setup GHTK clients from config
	ghtkCfg := b.config.App.GHTK
	ghtkClients := ghtk.NewClients(ghtkCfg.BaseURL, ghtkCfg.APIKey, ghtkCfg.ClientID, ghtkCfg.Secret)
	for _, c := range ghtkClients {
		b.transportMap[c.Config().ID] = c
		options = append(options, c.Config())
	}

	// Register transport options in background — Restate may not be ready at init time
	go func() {
		if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
			Category: "transport",
			Configs:  options,
		}); err != nil {
			slog.Warn("register transport options: %v", slog.Any("error", err))
		}
	}()

	return nil
}

func (b *OrderHandler) getTransportClient(option string) (transport.Client, error) {
	client, ok := b.transportMap[option]
	if !ok {
		return nil, ordermodel.ErrUnknownTransportOption
	}
	return client, nil
}

package orderbiz

import (
	"context"
	"log/slog"

	"shopnexus-server/internal/infras/transport"
	"shopnexus-server/internal/infras/transport/ghtk"
	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func (b *OrderHandler) SetupTransportMap() error {
	var options []sharedmodel.OptionConfig
	b.transportMap = make(map[string]transport.Client)

	// Setup GHTK clients
	// TODO: Load API keys from config or environment
	ghtkClients := ghtk.NewClients("https://services.ghtklab.com", "your-api-key", "your-client-id")
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

package orderbiz

import (
	"context"
	"log/slog"

	"shopnexus-server/config"
	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/payment/cod"
	"shopnexus-server/internal/provider/payment/vnpay"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func (b *OrderHandler) SetupPaymentMap() error {
	var configs []sharedmodel.OptionConfig

	b.paymentMap = make(map[string]payment.Client) // map[gatewayID]payment.Client

	// setup cod client
	codClient := cod.NewClient()
	b.paymentMap[codClient.Config().ID] = codClient
	configs = append(configs, codClient.Config())

	// setup vnpay client
	vnpayClients := vnpay.NewClients(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  config.GetConfig().App.Vnpay.ReturnURL,
	})
	for _, c := range vnpayClients {
		b.paymentMap[c.Config().ID] = c
		configs = append(configs, c.Config())
	}

	// Register payment options in background — Restate may not be ready at init time
	go func() {
		if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
			Category: "payment",
			Configs:  configs,
		}); err != nil {
			slog.Warn("register payment options: %v", slog.Any("error", err))
		}
	}()

	return nil
}

func (b *OrderHandler) getPaymentClient(option string) (payment.Client, error) {
	client, ok := b.paymentMap[option]
	if !ok {
		return nil, ordermodel.ErrUnknownPaymentOption.Fmt(option).Terminal()
	}
	return client, nil
}

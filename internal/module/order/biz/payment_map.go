package orderbiz

import (
	"context"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/payment"
	"shopnexus-server/internal/infras/payment/cod"
	"shopnexus-server/internal/infras/payment/vnpay"
	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func (b *OrderBiz) SetupPaymentMap() error {
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

	// TODO: use message queue to update
	if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
		Category: "payment",
		Configs:  configs,
	}); err != nil {
		return err
	}

	return nil
}

func (b *OrderBiz) getPaymentClient(option string) (payment.Client, error) {
	client, ok := b.paymentMap[option]
	if !ok {
		return nil, ordermodel.ErrUnknownPaymentOption.Fmt(option)
	}
	return client, nil
}

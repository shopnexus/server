package orderbiz

import (
	"context"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/payment"
	"shopnexus-remastered/internal/infras/payment/cod"
	"shopnexus-remastered/internal/infras/payment/vnpay"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonmodel "shopnexus-remastered/internal/shared/model"
)

func (b *OrderBiz) SetupPaymentMap() error {
	var configs []commonmodel.OptionConfig

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

	if err := b.common.UpdateServiceOptions(context.Background(), commonbiz.UpdateServiceOptionsParams{
		// TODO: should use message queue to update
		Category: "payment",
		Configs:  configs,
	}); err != nil {
		return err
	}

	return nil
}

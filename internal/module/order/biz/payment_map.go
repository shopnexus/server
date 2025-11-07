package orderbiz

import (
	"context"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/payment"
	"shopnexus-remastered/internal/client/payment/cod"
	"shopnexus-remastered/internal/client/payment/vnpay"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	commonmodel "shopnexus-remastered/internal/module/common/model"
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
		Storage:  b.storage,
		Category: "payment",
		Configs:  configs,
	}); err != nil {
		return err
	}

	return nil
}

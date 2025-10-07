package orderbiz

import (
	"context"
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/payment"
	"shopnexus-remastered/internal/client/payment/cod"
	"shopnexus-remastered/internal/client/payment/vnpay"
	sharedmodel "shopnexus-remastered/internal/module/shared/model"
)

func (s *OrderBiz) SetupPaymentMap() error {
	var configs []sharedmodel.OptionConfig

	s.paymentMap = make(map[string]payment.Client) // map[gatewayID]payment.Client

	// setup cod client
	codClient := cod.NewClient()
	s.paymentMap[codClient.Config().ID] = codClient
	configs = append(configs, codClient.Config())

	// setup vnpay client
	vnpayClients := vnpay.NewClients(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  config.GetConfig().App.Vnpay.ReturnURL,
	})
	for _, c := range vnpayClients {
		s.paymentMap[c.Config().ID] = c
		configs = append(configs, c.Config())
	}

	if err := s.shared.UpdateServiceOptions(context.Background(), "payment", configs); err != nil {
		return err
	}

	return nil
}

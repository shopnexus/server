package order

import (
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/payment"
	"shopnexus-remastered/internal/client/payment/vnpay"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	orderecho "shopnexus-remastered/internal/module/order/transport/echo"

	"go.uber.org/fx"
)

// Module provides the order module dependencies
var Module = fx.Module("order",
	fx.Provide(
		NewGatewayMap,
		orderbiz.NewOrderBiz,
		orderecho.NewHandler,
	),
)

func NewGatewayMap() (map[string]payment.Client, error) {
	m := make(map[string]payment.Client) // map[gatewayID]payment.Client

	m["cod"] = payment.NewCODClient()

	// setup vnpay client
	m["vnpay_card"] = vnpay.NewClient(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  config.GetConfig().App.Vnpay.ReturnURL,
	})

	m["vnpay_banktransfer"] = m["vnpay_card"]

	return m, nil
}

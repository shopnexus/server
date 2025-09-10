package order

import (
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/vnpay"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	orderecho "shopnexus-remastered/internal/module/order/transport/echo"

	"go.uber.org/fx"
)

// Module provides the order module dependencies
var Module = fx.Module("order",
	fx.Provide(
		NewVnpayClient,
		orderbiz.NewOrderBiz,
		orderecho.NewHandler,
	),
)

func NewVnpayClient() vnpay.Client {
	return vnpay.NewClient(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
	})
}

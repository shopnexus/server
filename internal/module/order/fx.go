package order

import (
	"encoding/json"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/client/vnpay"
	orderbiz "shopnexus-remastered/internal/module/order/biz"
	orderecho "shopnexus-remastered/internal/module/order/transport/echo"

	"go.uber.org/fx"
)

// Module provides the order module dependencies
var Module = fx.Module("order",
	fx.Provide(
		NewPusubClient,
		NewVnpayClient,
		orderbiz.NewOrderBiz,
		orderecho.NewHandler,
	),
)

func NewVnpayClient() vnpay.Client {
	return vnpay.NewClient(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  config.GetConfig().App.Vnpay.ReturnURL,
	})
}

func NewPusubClient() (pubsub.Client, error) {
	return pubsub.NewKafkaClient(pubsub.KafkaConfig{
		Config: pubsub.Config{
			Timeout: 10,
			Brokers: []string{"localhost:9092"},
			Decoder: json.Unmarshal,
			Encoder: json.Marshal,
		},
		Group: "order_service",
	})
}

package main

import (
	"context"
	"fmt"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/vnpay"
)

func main() {
	client := vnpay.NewClient(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  config.GetConfig().App.Vnpay.ReturnURL,
	})

	url, err := client.CreateOrder(context.TODO(), vnpay.CreateOrderParams{
		RefID:  13,
		Amount: 100000,
		Info:   "Don hang 1",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(url)
}

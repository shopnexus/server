package main

import (
	"context"
	"fmt"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/payment"
	"shopnexus-remastered/internal/client/payment/vnpay"
)

func main() {
	client := vnpay.NewClient(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  "localhost",
	})

	url, err := client.CreateOrder(context.TODO(), payment.CreateOrderParams{
		RefID:  13,
		Amount: 100000,
		Info:   "Don hang 1",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(url)
}

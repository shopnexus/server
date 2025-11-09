package main

import (
	"context"
	"fmt"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/payment"
	"shopnexus-remastered/internal/infras/payment/vnpay"
)

func main() {
	client := vnpay.NewClients(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  "localhost",
	})[0]

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

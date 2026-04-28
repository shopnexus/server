package main

import (
	"context"
	"fmt"

	"shopnexus-server/config"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/payment/vnpay"
)

func main() {
	client := vnpay.NewClients(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  "localhost",
	})[0]

	result, err := client.Create(context.TODO(), payment.CreateParams{
		RefID:       "13",
		Amount:      100000,
		Description: "Don hang 1",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.RedirectURL)
}

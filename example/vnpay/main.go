package main

import (
	"context"
	"encoding/json"
	"fmt"

	"shopnexus-server/config"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/payment/vnpay"
	sharedmodel "shopnexus-server/internal/shared/model"
)

func main() {
	data, _ := json.Marshal(vnpay.Data{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  "localhost",
		Method:     vnpay.MethodQR,
	})
	client := vnpay.NewClient(sharedmodel.Option{
		ID:       "vnpay_qr",
		Type:     sharedmodel.OptionTypePayment,
		Provider: "vnpay",
		Data:     data,
	})

	result, err := client.Charge(context.TODO(), payment.ChargeParams{
		RefID:       "13",
		Amount:      100000,
		Description: "Don hang 1",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.RedirectURL)
}

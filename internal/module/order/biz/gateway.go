package orderbiz

import (
	"context"
	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/client/payment"
	"shopnexus-remastered/internal/client/payment/vnpay"
	"shopnexus-remastered/internal/db"
	"shopnexus-remastered/internal/utils/pgutil"
)

func (s *OrderBiz) SetupPaymentGateway() error {
	ctx := context.Background()

	s.gatewayMap = make(map[string]payment.Client) // map[gatewayID]payment.Client

	s.gatewayMap["cod"] = payment.NewCODClient()

	// setup vnpay client
	s.gatewayMap["vnpay_card"] = vnpay.NewClient(vnpay.ClientOptions{
		TmnCode:    config.GetConfig().App.Vnpay.TmnCode,
		HashSecret: config.GetConfig().App.Vnpay.HashSecret,
		ReturnURL:  config.GetConfig().App.Vnpay.ReturnURL,
	})

	s.gatewayMap["vnpay_banktransfer"] = s.gatewayMap["vnpay_card"]

	// Ensure all supported gateway are in the database
	var supportedGateways = []struct {
		ID          string
		Method      db.OrderPaymentMethod
		Description string
	}{
		{ID: "cod", Method: db.OrderPaymentMethodCOD},
		{ID: "vnpay_card", Method: db.OrderPaymentMethodCard},
		{ID: "vnpay_banktransfer", Method: db.OrderPaymentMethodBankTransfer},
	}
	for _, gateway := range supportedGateways {
		if _, err := s.storage.GetOrderPaymentGateway(ctx, pgutil.StringToPgText(gateway.ID)); err != nil {
			if _, err := s.storage.CreateDefaultOrderPaymentGateway(ctx, db.CreateDefaultOrderPaymentGatewayParams{
				ID:          gateway.ID,
				Method:      gateway.Method,
				Description: pgutil.StringToPgText(gateway.Description),
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

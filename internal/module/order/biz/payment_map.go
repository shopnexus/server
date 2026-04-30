package orderbiz

import (
	"context"
	"encoding/json"
	"log/slog"

	commonbiz "shopnexus-server/internal/module/common/biz"
	ordermodel "shopnexus-server/internal/module/order/model"
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/payment/card"
	"shopnexus-server/internal/provider/payment/sepay"
	"shopnexus-server/internal/provider/payment/vnpay"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// SetupPaymentMap registers the payment options in the central catalog.
// Clients themselves are built on demand — nothing is cached on the handler.
func (b *OrderHandler) SetupPaymentMap() error {
	configs := b.paymentConfigs()

	go func() {
		if err := b.common.UpsertOptions(context.Background(), commonbiz.UpsertOptionsParams{
			Category: string(sharedmodel.OptionTypePayment),
			Configs:  configs,
		}); err != nil {
			slog.Warn("register payment options", slog.Any("error", err))
		}
	}()

	return nil
}

// paymentFactory routes a payment Option to its provider-specific constructor.
func paymentFactory(cfg sharedmodel.Option) payment.Client {
	switch cfg.Provider {
	case "vnpay":
		return vnpay.NewClient(cfg)
	case "sepay":
		return sepay.NewClient(cfg)
	case "card":
		return card.NewClient(cfg)
	default:
		slog.Warn("unknown payment provider", "provider", cfg.Provider, "id", cfg.ID)
		return nil
	}
}

func (b *OrderHandler) paymentConfigs() []sharedmodel.Option {
	var configs []sharedmodel.Option

	vnpayCfg := b.config.App.Vnpay
	for _, method := range []string{vnpay.MethodQR, vnpay.MethodBank, vnpay.MethodATM} {
		data, _ := json.Marshal(vnpay.Data{
			TmnCode:    vnpayCfg.TmnCode,
			HashSecret: vnpayCfg.HashSecret,
			ReturnURL:  vnpayCfg.ReturnURL,
			Method:     method,
		})
		configs = append(configs, sharedmodel.Option{
			ID:       "vnpay_" + method,
			Type:     sharedmodel.OptionTypePayment,
			Provider: "vnpay",
			Name:     "VNPay - " + method,
			Data:     data,
		})
	}

	if c := b.config.App.Sepay; c.MerchantID != "" {
		data, _ := json.Marshal(sepay.Data{
			MerchantID:   c.MerchantID,
			SecretKey:    c.SecretKey,
			IPNSecretKey: c.IPNSecretKey,
			SuccessURL:   c.SuccessURL,
			ErrorURL:     c.ErrorURL,
			CancelURL:    c.CancelURL,
			Sandbox:      c.Sandbox,
		})
		configs = append(configs, sharedmodel.Option{
			ID:       "sepay_bank_transfer",
			Type:     sharedmodel.OptionTypePayment,
			Provider: "sepay",
			Name:     "SePay - Bank Transfer",
			Data:     data,
		})
	}

	if c := b.config.App.CardPayment; c.Provider != "" {
		data, _ := json.Marshal(card.Data{
			Processor: c.Provider,
			SecretKey: c.SecretKey,
			PublicKey: c.PublicKey,
		})
		configs = append(configs, sharedmodel.Option{
			ID:       "card_" + c.Provider,
			Type:     sharedmodel.OptionTypePayment,
			Provider: "card",
			Name:     "Card Payment (" + c.Provider + ")",
			Data:     data,
		})
	}

	return configs
}

// getPaymentClient builds a payment client on demand for the given option ID.
// The lookup walks the config-derived option list — no per-handler cache.
func (b *OrderHandler) getPaymentClient(option string) (payment.Client, error) {
	for _, cfg := range b.paymentConfigs() {
		if cfg.ID == option {
			if client := paymentFactory(cfg); client != nil {
				return client, nil
			}
			break
		}
	}
	return nil, ordermodel.ErrUnknownPaymentOption.Fmt(option).Terminal()
}

package orderecho

import (
	"shopnexus-server/internal/provider/payment"
	"shopnexus-server/internal/provider/payment/card"
	"shopnexus-server/internal/provider/payment/sepay"
	"shopnexus-server/internal/provider/payment/vnpay"
	sharedmodel "shopnexus-server/internal/shared/model"
)

// newPaymentClient dispatches Option to the matching provider; nil if unknown.
func newPaymentClient(opt sharedmodel.Option) payment.Client {
	switch opt.Provider {
	case "sepay":
		return sepay.NewClient(opt)
	case "vnpay":
		return vnpay.NewClient(opt)
	case "card":
		return card.NewClient(opt)
	default:
		return nil
	}
}

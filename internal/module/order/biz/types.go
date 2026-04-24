package orderbiz

import "time"

const (
	TxTypeCheckout   = "checkout"
	TxTypeConfirmFee = "confirm_fee"
	TxTypePayout     = "payout"
	TxTypeRefund     = "refund"

	paymentExpiry = 30 * time.Minute
	escrowWindow  = 7 * 24 * time.Hour
)

package orderbiz

import "time"

// SessionKind values mirror the strings stored in payment_session.kind.
// The DB column is plain TEXT (not an enum) — these constants are the source of truth.
const (
	SessionKindBuyerCheckout         = "buyer-checkout"
	SessionKindSellerConfirmationFee = "seller-confirmation-fee"
	SessionKindSellerPayout          = "seller-payout"
)

const (
	paymentExpiry = 30 * time.Minute
	escrowWindow  = 7 * 24 * time.Hour
)

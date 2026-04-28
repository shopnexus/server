package orderbiz

import (
	"time"

	accountmodel "shopnexus-server/internal/module/account/model"

	"github.com/google/uuid"
)

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

// CheckoutWorkflowInput is the payload submitted to CheckoutWorkflow.Run.
// The session UUID is derived from the workflow ID (ctx.Key()) inside Run —
// the HTTP transport pre-allocates a UUID, uses its string form as the
// workflow ID, and passes that ID to restate.Workflow(...).Submit; the
// workflow then parses ctx.Key() back into the session UUID.
type CheckoutWorkflowInput struct {
	Account       accountmodel.AuthenticatedAccount `json:"account"`
	Items         []CheckoutItem                    `json:"items" validate:"required,min=1,dive"`
	Address       string                            `json:"address" validate:"required,min=1,max=500"`
	BuyNow        bool                              `json:"buy_now"`
	UseWallet     bool                              `json:"use_wallet"`
	WalletID      *uuid.UUID                        `json:"wallet_id,omitempty"`
	PaymentOption string                            `json:"payment_option" validate:"max=100"`
}

// CheckoutWorkflowOutput is the terminal value returned from CheckoutWorkflow.Run.
// Status is one of "paid", "expired", "cancelled".
type CheckoutWorkflowOutput struct {
	Status    string    `json:"status"`
	SessionID uuid.UUID `json:"session_id"`
}

package orderbiz

import (
	"encoding/json"
	"time"

	accountmodel "shopnexus-server/internal/module/account/model"
	"shopnexus-server/internal/provider/payment"

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

type CheckoutWorkflowInput struct {
	Account       accountmodel.AuthenticatedAccount `json:"account"`
	Items         []CheckoutItem                    `json:"items" validate:"required,min=1,dive"`
	Address       string                            `json:"address" validate:"required,min=1,max=500"`
	BuyNow        bool                              `json:"buy_now"`
	UseWallet     bool                              `json:"use_wallet"`
	WalletID      *uuid.UUID                        `json:"wallet_id,omitempty"`
	PaymentOption string                            `json:"payment_option" validate:"max=100"`
}

type CheckoutWorkflowOutput struct {
	Status    string    `json:"status"`
	SessionID uuid.UUID `json:"session_id"`
}

type ConfirmWorkflowInput struct {
	Account       accountmodel.AuthenticatedAccount `json:"account"`
	ItemIDs       []int64                           `json:"item_ids" validate:"required,min=1,max=1000"`
	UseWallet     bool                              `json:"use_wallet"`
	WalletID      *uuid.UUID                        `json:"wallet_id,omitempty"`
	PaymentOption string                            `json:"payment_option" validate:"max=100"`
	Note          string                            `json:"note" validate:"max=500"`
}

type ConfirmWorkflowOutput struct {
	Status           string    `json:"status"`
	OrderID          uuid.UUID `json:"order_id,omitempty"`
	ConfirmSessionID uuid.UUID `json:"confirm_session_id"`
}

type PayoutInput struct {
	OrderID   uuid.UUID `json:"order_id"`
	SellerID  uuid.UUID `json:"seller_id"`
	PaidTotal int64     `json:"paid_total"`
	Currency  string    `json:"currency"`
}

type PayoutOutput struct {
	OrderID uuid.UUID `json:"order_id"`
	Outcome string    `json:"outcome"`
}

type OnPaymentResultParams struct {
	SessionID    uuid.UUID       `json:"session_id" validate:"required"`
	TxID         int64           `json:"tx_id"`
	Status       payment.Status  `json:"status" validate:"required"`
	ProviderData json.RawMessage `json:"provider_data,omitempty"`
}

type RefundSnapshot struct {
	HasActiveRefund    bool `json:"has_active_refund"`
	LastRefundApproved bool `json:"last_refund_approved"`
}

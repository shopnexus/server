package orderbiz

import (
	"strconv"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
)

// PaymentEvent is a discriminated union resolved into the workflow's
// `payment_event` DurablePromise. Kind: "paid" | "failed" | "cancelled".
type PaymentEvent struct {
	Kind   string `json:"kind"`
	TxID   int64  `json:"tx_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// CheckoutWorkflowID stringifies an int64 session ID for use as workflow ID.
func CheckoutWorkflowID(sessionID int64) string {
	return strconv.FormatInt(sessionID, 10)
}

// ConfirmWorkflowID stringifies an int64 confirm-session ID.
func ConfirmWorkflowID(sessionID int64) string {
	return strconv.FormatInt(sessionID, 10)
}

// WorkflowForSession maps payment_session.kind to (workflowName, workflowID).
// Returns ("", "") for sessions that have no associated workflow (e.g. payout).
func WorkflowForSession(s orderdb.OrderPaymentSession) (workflowName, workflowID string) {
	switch s.Kind {
	case SessionKindBuyerCheckout:
		return "CheckoutWorkflow", CheckoutWorkflowID(s.ID)
	case SessionKindSellerConfirmationFee:
		return "ConfirmWorkflow", ConfirmWorkflowID(s.ID)
	default:
		return "", ""
	}
}

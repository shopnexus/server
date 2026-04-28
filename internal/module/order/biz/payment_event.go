package orderbiz

import (
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
)

// PaymentEvent is a discriminated union resolved into the workflow's
// `payment_event` DurablePromise. Kind: "paid" | "failed" | "cancelled".
type PaymentEvent struct {
	Kind   string `json:"kind"`
	TxID   int64  `json:"tx_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// WorkflowForSession maps payment_session.kind to (workflowName, workflowID).
// Returns ("", "") for sessions outside the workflow scheme (e.g. payout).
// The workflow ID is the session UUID stringified — both workflow.Run and the
// HTTP submission path agree on this convention so webhook routing is a
// direct map (RefID == workflow ID) without a DB lookup.
func WorkflowForSession(s orderdb.OrderPaymentSession) (workflowName, workflowID string) {
	switch s.Kind {
	case SessionKindBuyerCheckout:
		return "CheckoutWorkflow", s.ID.String()
	case SessionKindSellerConfirmationFee:
		return "ConfirmWorkflow", s.ID.String()
	default:
		return "", ""
	}
}

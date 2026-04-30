package orderbiz

import (
	"encoding/json"

	"github.com/google/uuid"
	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"
	"shopnexus-server/internal/shared/validator"
)

// OnPaymentResult is the unified entry point for gateway IPN webhooks.
func (b *OrderHandler) OnPaymentResult(ctx restate.Context, params payment.Notification) error {
	if err := validator.Validate(params); err != nil {
		return sharedmodel.WrapErr("validate on payment result", err)
	}

	txID, err := uuid.Parse(params.RefID)
	if err != nil {
		return sharedmodel.WrapErr("parse tx id", err)
	}

	tx, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderTransaction, error) {
		return b.storage.Querier().GetTransaction(rctx, uuid.NullUUID{UUID: txID, Valid: true})
	})
	if err != nil {
		return sharedmodel.WrapErr("get transaction", err)
	}

	// load session + resolve TxID if the webhook didn't supply one.
	session, err := restate.Run(ctx, func(rctx restate.RunContext) (orderdb.OrderPaymentSession, error) {
		return b.storage.Querier().GetPaymentSession(rctx, uuid.NullUUID{UUID: tx.SessionID, Valid: true})
	})
	if err != nil {
		return sharedmodel.WrapErr("get session", err)
	}

	wfName, wfID := WorkflowForSession(session)
	if wfName == "" {
		return nil
	}

	// signal owning workflow's payment_event promise.
	restate.WorkflowSend(ctx, wfName, wfID, "PaymentNotification").Send(params)
	return nil
}

// WorkflowForSession maps payment_session.kind to (workflowName, workflowID).
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

type InitGatewayPaymentParams struct {
	TxID          uuid.UUID
	Amount        int64
	PaymentOption string
	Description   string
}

// InitGatewayPayment creates a gateway payment
func (b *OrderHandler) InitGatewayPayment(
	ctx restate.Context,
	params InitGatewayPaymentParams,
) (string, error) {
	if params.Amount <= 0 {
		return "", nil
	}

	paymentClient, err := b.getPaymentClient(params.PaymentOption)
	if err != nil {
		return "", sharedmodel.WrapErr("get payment client", err)
	}

	url, err := restate.Run(ctx, func(rctx restate.RunContext) (string, error) {
		r, e := paymentClient.Charge(rctx, payment.ChargeParams{
			RefID:       params.TxID.String(),
			Amount:      params.Amount,
			Description: params.Description,
		})
		if e != nil {
			return "", e
		}
		return r.RedirectURL, nil
	})
	if err != nil {
		return "", sharedmodel.WrapErr("create gateway payment", err)
	}

	if url != "" {
		if pErr := restate.RunVoid(ctx, func(rctx restate.RunContext) error {
			data, _ := json.Marshal(map[string]string{"gateway_url": url})
			return b.storage.Querier().SetTransactionData(rctx, orderdb.SetTransactionDataParams{
				ID:   params.TxID,
				Data: data,
			})
		}); pErr != nil {
			return "", sharedmodel.WrapErr("persist gateway url on tx", pErr)
		}
	}

	return url, nil
}

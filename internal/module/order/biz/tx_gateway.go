package orderbiz

import (
	"encoding/json"

	restate "github.com/restatedev/sdk-go"

	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	"shopnexus-server/internal/provider/payment"
	sharedmodel "shopnexus-server/internal/shared/model"
)

type InitGatewayPaymentParams struct {
	Amount        int64
	PaymentOption string
	BlockerTxID   int64
	RefID         string
	Description   string
}

// InitGatewayPayment creates a gateway payment for `params.Amount` and persists
// the resulting redirect URL onto the blocker transaction's `data` JSON column.
// Returns the URL (empty string if Amount is 0 — caller should still resolve any
// dependent durable promises with "" so HTTP attach callers don't hang).
//
// Used by CheckoutWorkflow and ConfirmWorkflow. The caller is responsible for
// resolving the `payment_url` durable promise after this returns.
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
		r, e := paymentClient.Create(rctx, payment.CreateParams{
			RefID:       params.RefID,
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
				ID:   params.BlockerTxID,
				Data: data,
			})
		}); pErr != nil {
			return "", sharedmodel.WrapErr("persist gateway url on tx", pErr)
		}
	}

	return url, nil
}

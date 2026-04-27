package orderbiz

import (
	"fmt"

	"github.com/google/uuid"
	restate "github.com/restatedev/sdk-go"

	accountbiz "shopnexus-server/internal/module/account/biz"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
)

type CreditFromSessionParams struct {
	SessionID  int64     `json:"session_id"`
	AccountID  uuid.UUID `json:"account_id"`
	CreditType string    `json:"credit_type"`
	Reference  string    `json:"reference"`
	Note       string    `json:"note"`
}

// CreditFromSession credits the recipient with the sum of positive-amount Success
// transactions in the given session. Use this when a session is being voided or
// refunded — credits only legs that actually settled, never minting balance for
// unsettled / failed / pending legs. Returns the amount credited; 0 means no-op.
func (b *OrderHandler) CreditFromSession(
	ctx restate.Context,
	params CreditFromSessionParams,
) (int64, error) {
	settled, err := restate.Run(ctx, func(ctx restate.RunContext) (int64, error) {
		txs, err := b.storage.Querier().ListTransactionsBySession(ctx, params.SessionID)
		if err != nil {
			return 0, sharedmodel.WrapErr("list session txs", err)
		}
		var total int64
		for _, tx := range txs {
			if tx.Status == orderdb.OrderStatusSuccess && tx.Amount > 0 {
				total += tx.Amount
			}
		}
		return total, nil
	})
	if err != nil {
		return 0, err
	}
	if settled == 0 {
		return 0, nil
	}

	if err := b.account.WalletCredit(ctx, accountbiz.WalletCreditParams{
		AccountID: params.AccountID,
		Amount:    settled,
		Type:      params.CreditType,
		Reference: fmt.Sprintf("session:%d %s", params.SessionID, params.Reference),
		Note:      params.Note,
	}); err != nil {
		return 0, sharedmodel.WrapErr("wallet credit from session", err)
	}
	return settled, nil
}

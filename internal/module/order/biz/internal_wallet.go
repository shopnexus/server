package orderbiz

import (
	"context"
	orderdb "shopnexus-server/internal/module/order/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
	restate "github.com/restatedev/sdk-go"
)

// WalletDebitParams holds the parameters for debiting a wallet.
type WalletDebitParams struct {
	AccountID uuid.UUID `json:"account_id" validate:"required"`
	Amount    int64     `json:"amount" validate:"required,gt=0"`
	Reference string    `json:"reference"`
	Note      string    `json:"note"`
}

// WalletDebitResult holds the result of a wallet debit operation.
type WalletDebitResult struct {
	Deducted int64 `json:"deducted"`
	Balance  int64 `json:"balance"`
}

// WalletCreditParams holds the parameters for crediting a wallet.
type WalletCreditParams struct {
	AccountID uuid.UUID `json:"account_id" validate:"required"`
	Amount    int64     `json:"amount" validate:"required,gt=0"`
	Type      string    `json:"type" validate:"required"`
	Reference string    `json:"reference"`
	Note      string    `json:"note"`
}

// GetWalletBalance returns the profile balance (internal money) for the given account.
func (b *OrderHandler) GetWalletBalance(ctx restate.Context, accountID uuid.UUID) (int64, error) {
	balance, err := restate.Run(ctx, func(ctx restate.RunContext) (int64, error) {
		return b.storage.Querier().GetInternalBalance(ctx, accountID)
	})
	if err != nil {
		return 0, sharedmodel.WrapErr("get profile balance", err)
	}
	return balance, nil
}

// walletDebit deducts min(balance, amount).
func (b *OrderHandler) walletDebit(ctx context.Context, params WalletDebitParams) (WalletDebitResult, error) {
	txStorage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return WalletDebitResult{}, sharedmodel.WrapErr("begin wallet debit tx", err)
	}

	old, err := txStorage.Querier().GetInternalBalance(ctx, params.AccountID)
	if err != nil {
		return WalletDebitResult{}, sharedmodel.WrapErr("get wallet balance for debit", err)
	}

	newBalance, err := txStorage.Querier().DebitInternalWallet(ctx, orderdb.DebitInternalWalletParams{
		ID:     params.AccountID,
		Amount: params.Amount,
	})
	if err != nil {
		return WalletDebitResult{}, sharedmodel.WrapErr("debit wallet", err)
	}

	if err = txStorage.Commit(ctx); err != nil {
		return WalletDebitResult{}, sharedmodel.WrapErr("commit wallet debit tx", err)
	}
	return WalletDebitResult{Deducted: old - newBalance, Balance: newBalance}, nil
}

// walletCredit adds the given amount to profile.balance.
func (b *OrderHandler) walletCredit(ctx context.Context, params WalletCreditParams) error {
	_, err := b.storage.Querier().CreditInternalWallet(ctx, orderdb.CreditInternalWalletParams{
		ID:     params.AccountID,
		Amount: params.Amount,
	})
	if err != nil {
		return sharedmodel.WrapErr("credit wallet", err)
	}
	return nil
}

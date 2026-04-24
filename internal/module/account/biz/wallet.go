package accountbiz

import (
	"encoding/json"

	"github.com/google/uuid"
	restate "github.com/restatedev/sdk-go"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	sharedmodel "shopnexus-server/internal/shared/model"
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
// The actual money ledger lives in order.transaction; this returns the cached balance column.
func (b *AccountHandler) GetWalletBalance(ctx restate.Context, accountID uuid.UUID) (int64, error) {
	balance, err := restate.Run(ctx, func(ctx restate.RunContext) (int64, error) {
		return b.storage.Querier().GetProfileBalance(ctx, accountID)
	})
	if err != nil {
		return 0, sharedmodel.WrapErr("get profile balance", err)
	}
	return balance, nil
}

// WalletDebit deducts min(balance, amount) from profile.balance. The ledger entry itself
// is created by the calling module (e.g. order.transaction); this handler only adjusts
// the cached balance. Returns the amount actually deducted and the new balance.
func (b *AccountHandler) WalletDebit(ctx restate.Context, params WalletDebitParams) (WalletDebitResult, error) {
	result, err := restate.Run(ctx, func(ctx restate.RunContext) (WalletDebitResult, error) {
		old, err := b.storage.Querier().GetProfileBalance(ctx, params.AccountID)
		if err != nil {
			return WalletDebitResult{}, err
		}
		newBalance, err := b.storage.Querier().DebitProfileBalance(ctx, accountdb.DebitProfileBalanceParams{
			ID:     params.AccountID,
			Amount: params.Amount,
		})
		if err != nil {
			return WalletDebitResult{}, err
		}
		return WalletDebitResult{Deducted: old - newBalance, Balance: newBalance}, nil
	})
	if err != nil {
		return WalletDebitResult{}, sharedmodel.WrapErr("wallet debit", err)
	}
	return result, nil
}

// WalletCredit adds the given amount to profile.balance. Ledger entry is recorded by
// the calling module (e.g. order.transaction `refund` / `payout`).
func (b *AccountHandler) WalletCredit(ctx restate.Context, params WalletCreditParams) error {
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().CreditProfileBalance(ctx, accountdb.CreditProfileBalanceParams{
			ID:     params.AccountID,
			Amount: params.Amount,
		})
		return err
	}); err != nil {
		return sharedmodel.WrapErr("wallet credit", err)
	}
	return nil
}

// --- Payment instrument (account.wallet) CRUD ---

type CreateWalletParams struct {
	AccountID uuid.UUID       `json:"account_id" validate:"required"`
	Option    string          `json:"option" validate:"required,max=100"`
	Label     string          `json:"label" validate:"required,max=100"`
	Data      json.RawMessage `json:"data"`
}

type ListWalletsParams struct {
	AccountID uuid.UUID `json:"account_id" validate:"required"`
}

type DeleteWalletParams struct {
	AccountID uuid.UUID `json:"account_id" validate:"required"`
	WalletID  uuid.UUID `json:"wallet_id" validate:"required"`
}

// CreateWallet stores a new payment instrument for the account (card token, e-wallet ref, etc.).
func (b *AccountHandler) CreateWallet(ctx restate.Context, params CreateWalletParams) (accountdb.AccountWallet, error) {
	wallet, err := restate.Run(ctx, func(ctx restate.RunContext) (accountdb.AccountWallet, error) {
		return b.storage.Querier().CreateDefaultWallet(ctx, accountdb.CreateDefaultWalletParams{
			AccountID: params.AccountID,
			Option:    params.Option,
			Label:     params.Label,
			Data:      params.Data,
		})
	})
	if err != nil {
		return wallet, sharedmodel.WrapErr("create wallet", err)
	}
	return wallet, nil
}

// ListWallets returns all payment instruments for an account.
func (b *AccountHandler) ListWallets(ctx restate.Context, params ListWalletsParams) ([]accountdb.AccountWallet, error) {
	rows, err := restate.Run(ctx, func(ctx restate.RunContext) ([]accountdb.AccountWallet, error) {
		return b.storage.Querier().ListWallet(ctx, accountdb.ListWalletParams{
			AccountID: []uuid.UUID{params.AccountID},
		})
	})
	if err != nil {
		return nil, sharedmodel.WrapErr("list wallets", err)
	}
	return rows, nil
}

// DeleteWallet removes a payment instrument. Ownership is enforced by the composite filter.
func (b *AccountHandler) DeleteWallet(ctx restate.Context, params DeleteWalletParams) error {
	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		return b.storage.Querier().DeleteWallet(ctx, accountdb.DeleteWalletParams{
			ID:        []uuid.UUID{params.WalletID},
			AccountID: []uuid.UUID{params.AccountID},
		})
	}); err != nil {
		return sharedmodel.WrapErr("delete wallet", err)
	}
	return nil
}

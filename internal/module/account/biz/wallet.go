package accountbiz

import (
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

// WalletDebit deducts min(balance, amount) from the wallet, records a transaction,
// and returns the amount actually deducted and the new balance.
func (b *AccountHandler) WalletDebit(ctx restate.Context, params WalletDebitParams) (WalletDebitResult, error) {
	// TODO(account-refactor): re-implement against profile.balance + order.transaction ledger.
	return WalletDebitResult{Deducted: 0, Balance: 0}, nil
}

// WalletCredit upserts the wallet and credits the given amount, recording a transaction.
func (b *AccountHandler) WalletCredit(ctx restate.Context, params WalletCreditParams) error {
	// TODO(account-refactor): re-implement against profile.balance + order.transaction ledger.
	return nil
}

// GetWalletBalance returns the wallet balance for the given account ID.
func (b *AccountHandler) GetWalletBalance(ctx restate.Context, accountID uuid.UUID) (int64, error) {
	// TODO(account-refactor): re-implement against profile.balance.
	return 0, nil
}

// ListWalletTransactions returns paginated wallet transactions for the given account.
func (b *AccountHandler) ListWalletTransactions(ctx restate.Context, params ListWalletTransactionsParams) ([]WalletTransactionResult, error) {
	// TODO(account-refactor): re-implement against order.transaction ledger.
	return nil, nil
}

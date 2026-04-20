package accountbiz

import (
	"errors"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5"
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

// GetWalletBalance returns the wallet balance for the given account ID.
// Returns 0 if no wallet exists yet.
func (b *AccountHandler) GetWalletBalance(ctx restate.Context, accountID uuid.UUID) (int64, error) {
	wallet, err := b.storage.Querier().GetWallet(ctx, accountdb.GetWalletParams{
		AccountID: uuid.NullUUID{UUID: accountID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, sharedmodel.WrapErr("get wallet balance", err)
	}
	return wallet.Balance, nil
}

// WalletDebit deducts min(balance, amount) from the wallet, records a transaction,
// and returns the amount actually deducted and the new balance.
func (b *AccountHandler) WalletDebit(ctx restate.Context, params WalletDebitParams) (WalletDebitResult, error) {
	var zero WalletDebitResult

	type dbResult struct {
		OldBalance int64 `json:"old_balance"`
		NewBalance int64 `json:"new_balance"`
	}

	result, err := restate.Run(ctx, func(ctx restate.RunContext) (dbResult, error) {
		// Get current balance to compute deducted amount
		wallet, err := b.storage.Querier().GetWallet(ctx, accountdb.GetWalletParams{
			AccountID: uuid.NullUUID{UUID: params.AccountID, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return dbResult{}, nil
			}
			return dbResult{}, err
		}
		oldBalance := wallet.Balance

		updated, err := b.storage.Querier().DebitWallet(ctx, accountdb.DebitWalletParams{
			AccountID: params.AccountID,
			Amount:    params.Amount,
		})
		if err != nil {
			return dbResult{}, err
		}

		_, err = b.storage.Querier().CreateWalletTransaction(ctx, accountdb.CreateWalletTransactionParams{
			AccountID:   params.AccountID,
			Type:        accountdb.AccountWalletTransactionTypePayment,
			Amount:      oldBalance - updated.Balance,
			ReferenceID: null.StringFromPtr(nullableString(params.Reference)),
			Note:        null.StringFromPtr(nullableString(params.Note)),
		})
		if err != nil {
			return dbResult{}, err
		}

		return dbResult{OldBalance: oldBalance, NewBalance: updated.Balance}, nil
	})
	if err != nil {
		return zero, sharedmodel.WrapErr("wallet debit", err)
	}

	return WalletDebitResult{
		Deducted: result.OldBalance - result.NewBalance,
		Balance:  result.NewBalance,
	}, nil
}

// WalletCredit upserts the wallet and credits the given amount, recording a transaction.
func (b *AccountHandler) WalletCredit(ctx restate.Context, params WalletCreditParams) error {
	txType := accountdb.AccountWalletTransactionTypeRefund
	switch params.Type {
	case string(accountdb.AccountWalletTransactionTypePayment):
		txType = accountdb.AccountWalletTransactionTypePayment
	case string(accountdb.AccountWalletTransactionTypeTopUp):
		txType = accountdb.AccountWalletTransactionTypeTopUp
	}

	if err := restate.RunVoid(ctx, func(ctx restate.RunContext) error {
		_, err := b.storage.Querier().CreditWallet(ctx, accountdb.CreditWalletParams{
			AccountID: params.AccountID,
			Amount:    params.Amount,
		})
		if err != nil {
			return err
		}

		_, err = b.storage.Querier().CreateWalletTransaction(ctx, accountdb.CreateWalletTransactionParams{
			AccountID:   params.AccountID,
			Type:        txType,
			Amount:      params.Amount,
			ReferenceID: null.StringFromPtr(nullableString(params.Reference)),
			Note:        null.StringFromPtr(nullableString(params.Note)),
		})
		return err
	}); err != nil {
		return sharedmodel.WrapErr("wallet credit", err)
	}

	return nil
}

// ListWalletTransactions returns paginated wallet transactions for the given account.
func (b *AccountHandler) ListWalletTransactions(ctx restate.Context, params ListWalletTransactionsParams) ([]WalletTransactionResult, error) {
	txs, err := restate.Run(ctx, func(ctx restate.RunContext) ([]WalletTransactionResult, error) {
		dbTxs, err := b.storage.Querier().ListWalletTransaction(ctx, accountdb.ListWalletTransactionParams{
			AccountID: []uuid.UUID{params.AccountID},
			Limit:     params.Limit,
			Offset:    params.Offset(),
		})
		if err != nil {
			return nil, err
		}
		results := make([]WalletTransactionResult, len(dbTxs))
		for i, tx := range dbTxs {
			var refID, note *string
			if tx.ReferenceID.Valid {
				refID = &tx.ReferenceID.String
			}
			if tx.Note.Valid {
				note = &tx.Note.String
			}
			results[i] = WalletTransactionResult{
				ID:          tx.ID,
				AccountID:   tx.AccountID.String(),
				Type:        string(tx.Type),
				Amount:      tx.Amount,
				ReferenceID: refID,
				Note:        note,
				DateCreated: tx.DateCreated.Format("2006-01-02T15:04:05Z07:00"),
			}
		}
		return results, nil
	})
	if err != nil {
		return nil, err
	}
	return txs, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

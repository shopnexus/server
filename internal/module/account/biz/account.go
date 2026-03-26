package accountbiz

import (
	restate "github.com/restatedev/sdk-go"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"

	"github.com/google/uuid"
)

// DeleteAccountParams holds the parameters for deleting an account.
type DeleteAccountParams struct {
	AccountID uuid.UUID
}

// DeleteAccount suspends the account with the given ID.
func (b *AccountHandler) DeleteAccount(ctx restate.Context, params DeleteAccountParams) error {
	if _, err := b.storage.Querier().UpdateAccount(ctx, accountdb.UpdateAccountParams{
		ID:     params.AccountID,
		Status: accountdb.NullAccountStatus{AccountStatus: accountdb.AccountStatusSuspended, Valid: true},
	}); err != nil {
		return err
	}
	return nil
}

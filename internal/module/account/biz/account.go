package accountbiz

import (
	"context"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"

	"github.com/google/uuid"
)

type DeleteAccountParams struct {
	AccountID uuid.UUID
}

func (b *AccountBiz) DeleteAccount(ctx context.Context, params DeleteAccountParams) error {
	if _, err := b.storage.Querier().UpdateAccount(ctx, accountdb.UpdateAccountParams{
		ID:     params.AccountID,
		Status: accountdb.NullAccountStatus{AccountStatus: accountdb.AccountStatusSuspended, Valid: true},
	}); err != nil {
		return err
	}
	return nil
}

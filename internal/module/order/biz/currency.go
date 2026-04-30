package orderbiz

import (
	"context"

	accountbiz "shopnexus-server/internal/module/account/biz"
	sharedcurrency "shopnexus-server/internal/shared/currency"
	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/google/uuid"
)

// InferCurrency fetches the profile for accountID and resolves its ISO 4217 currency code.
func (b *OrderHandler) InferCurrency(ctx context.Context, accountID uuid.UUID) (string, error) {
	prof, err := b.account.GetProfile(ctx, accountbiz.GetProfileParams{AccountID: accountID})
	if err != nil {
		return "", sharedmodel.WrapErr("get profile for currency", err)
	}
	cur, err := sharedcurrency.Infer(prof.Country)
	if err != nil {
		return "", sharedmodel.WrapErr("infer currency", err)
	}
	return cur, nil
}

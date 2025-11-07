package accountbiz

import (
	"context"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	"shopnexus-remastered/internal/utils/pgsqlc"
)

type AccountBiz struct {
	storage   pgsqlc.Storage
	pubsub    pubsub.Client
	common    *commonbiz.Commonbiz
	promotion *promotionbiz.PromotionBiz
}

// NewAccountBiz creates a new instance of AccountBiz.
func NewAccountBiz(
	storage pgsqlc.Storage,
	pubsub pubsub.Client,
	common *commonbiz.Commonbiz,
	promotion *promotionbiz.PromotionBiz,
) *AccountBiz {
	return &AccountBiz{
		storage:   storage,
		pubsub:    pubsub,
		common:    common,
		promotion: promotion,
	}
}

type DeleteAccountParams struct {
	AccountID int64
}

func (b *AccountBiz) DeleteAccount(ctx context.Context, params DeleteAccountParams) error {
	if _, err := b.storage.UpdateAccountBase(ctx, db.UpdateAccountBaseParams{
		ID:     params.AccountID,
		Status: db.NullAccountStatus{AccountStatus: db.AccountStatusSuspended, Valid: true},
	}); err != nil {
		return err
	}
	return nil
}

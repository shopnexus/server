package accountbiz

import (
	"context"

	"shopnexus-remastered/internal/client/pubsub"
	"shopnexus-remastered/internal/db"
	promotionbiz "shopnexus-remastered/internal/module/promotion/biz"
	sharedbiz "shopnexus-remastered/internal/module/shared/biz"
	"shopnexus-remastered/internal/utils/pgutil"
)

type AccountBiz struct {
	storage   *pgutil.Storage
	pubsub    pubsub.Client
	shared    *sharedbiz.SharedBiz
	promotion *promotionbiz.PromotionBiz
}

// NewAccountBiz creates a new instance of AccountBiz.
func NewAccountBiz(
	storage *pgutil.Storage,
	pubsub pubsub.Client,
	shared *sharedbiz.SharedBiz,
	promotion *promotionbiz.PromotionBiz,
) *AccountBiz {
	return &AccountBiz{
		storage:   storage,
		pubsub:    pubsub,
		shared:    shared,
		promotion: promotion,
	}
}

type DeleteAccountParams struct {
	AccountID int64
}

func (s *AccountBiz) DeleteAccount(ctx context.Context, params DeleteAccountParams) error {
	if _, err := s.storage.UpdateAccountBase(ctx, db.UpdateAccountBaseParams{
		ID:     params.AccountID,
		Status: db.NullAccountStatus{AccountStatus: db.AccountStatusSuspended, Valid: true},
	}); err != nil {
		return err
	}
	return nil
}

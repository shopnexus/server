package accountbiz

import (
	"context"
	"time"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/pubsub"
	accountdb "shopnexus-remastered/internal/module/account/db/sqlc"
	commonbiz "shopnexus-remastered/internal/module/common/biz"
	"shopnexus-remastered/internal/shared/pgsqlc"

	"github.com/google/uuid"
)

type AccountStorage = pgsqlc.Storage[*accountdb.Queries]

type AccountBiz struct {
	tokenDuration        time.Duration
	jwtSecret            []byte
	refreshTokenDuration time.Duration
	refreshSecret        []byte

	storage AccountStorage
	pubsub  pubsub.Client
	common  *commonbiz.CommonBiz
}

// NewAccountBiz creates a new instance of AccountBiz.
func NewAccountBiz(
	config *config.Config,
	storage AccountStorage,
	pubsub pubsub.Client,
	common *commonbiz.CommonBiz,
) *AccountBiz {
	return &AccountBiz{
		tokenDuration:        time.Duration(config.App.JWT.AccessTokenDuration * int64(time.Second)),
		jwtSecret:            []byte(config.App.JWT.Secret),
		refreshTokenDuration: time.Duration(config.App.JWT.RefreshTokenDuration * int64(time.Second)),
		refreshSecret:        []byte(config.App.JWT.RefreshSecret),

		storage: storage,
		pubsub:  pubsub,
		common:  common,
	}
}

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

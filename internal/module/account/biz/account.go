package accountbiz

import (
	"context"
	"fmt"
	"time"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/pubsub"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	commonbiz "shopnexus-server/internal/module/common/biz"
	"shopnexus-server/internal/shared/pgsqlc"

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

func (b *AccountBiz) WithTx(ctx context.Context, fn func(context.Context, *AccountBiz) error) error {
	storage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer storage.Rollback(ctx)

	biz := &AccountBiz{
		tokenDuration:        b.tokenDuration,
		jwtSecret:            b.jwtSecret,
		refreshTokenDuration: b.refreshTokenDuration,
		refreshSecret:        b.refreshSecret,

		storage: storage,
		pubsub:  b.pubsub,
		common:  b.common,
	}

	if err = fn(ctx, biz); err != nil {
		return fmt.Errorf("failed to execute function with transaction: %w", err)
	}

	if err = storage.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
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

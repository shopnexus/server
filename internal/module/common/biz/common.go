package commonbiz

import (
	"context"
	"errors"
	"fmt"
	"shopnexus-server/internal/infras/objectstore"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	"shopnexus-server/internal/shared/pgsqlc"
)

type CommonStorage = pgsqlc.Storage[*commondb.Queries]

type CommonBiz struct {
	storage        CommonStorage
	objectstoreMap map[string]objectstore.Client
}

func NewcommonBiz(storage CommonStorage) (*CommonBiz, error) {
	b := &CommonBiz{
		storage: storage,
	}

	return b, errors.Join(
		b.SetupObjectStore(),
	)
}

func (b *CommonBiz) WithTx(ctx context.Context, fn func(context.Context, *CommonBiz) error) error {
	storage, err := b.storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer storage.Rollback(ctx)

	biz := &CommonBiz{
		storage:        storage,
		objectstoreMap: b.objectstoreMap,
	}

	if err = fn(ctx, biz); err != nil {
		return fmt.Errorf("failed to execute function with transaction: %w", err)
	}

	if err = storage.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

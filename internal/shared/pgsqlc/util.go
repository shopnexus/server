package pgsqlc

import (
	"context"
	"fmt"
)

func WithTx[B any](
	ctx context.Context,
	storage Storage[any],
	newBiz func(Storage[any]) (B, error),
	fn func(context.Context, B),
) error {
	tx, err := storage.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	biz, err := newBiz(tx)
	if err != nil {
		return fmt.Errorf("failed to create biz with transaction: %w", err)
	}

	fn(ctx, biz)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

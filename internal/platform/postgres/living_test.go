package postgres_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestQueueLockLiving(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	gone := errors.New("gone")
	send := func(account uuid.UUID) error {
		return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			b := &pgx.Batch{}
			postgres.QueueLockLiving(b, account, gone)
			return tx.SendBatch(ctx, b).Close()
		})
	}
	if err := send(accounttest.New(t, db)); err != nil {
		t.Fatalf("a living account: %v", err)
	}
	if err := send(uuid.NewV7()); !errors.Is(err, gone) {
		t.Fatalf("no account: %v", err)
	}
	erased := accounttest.New(t, db)
	if _, err := db.Exec(ctx, `UPDATE accounts SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`, pgx.StrictNamedArgs{"id": erased}); err != nil {
		t.Fatal(err)
	}
	if err := send(erased); !errors.Is(err, gone) {
		t.Fatalf("a deleted account: %v", err)
	}
}

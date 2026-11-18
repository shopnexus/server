package repository_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// A save or a search that checked nothing before the account was deleted,
// and writes after discovery's worker cleared it, writes nothing.
func TestDeletedAccountGetsNoSaveOrHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	gone := accounttest.New(t, db)
	listing := listings(t, db, 1)[0]
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewSaves(db).Save(ctx, uuid.NewV7(), gone, listing, 1_000, now); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("save: %v", err)
	}
	h := repository.NewHistory(db)
	if err := h.Remember(ctx, gone, repository.HistoryEntry{Query: "xe đạp", SearchedAt: now}); err != nil {
		t.Fatalf("remember: %v", err)
	}
	if list, _ := h.List(ctx, gone); len(list) != 0 {
		t.Fatalf("history of a deleted account: %+v", list)
	}
}

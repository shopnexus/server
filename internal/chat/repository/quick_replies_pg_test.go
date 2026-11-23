package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// An account that never saved quick replies reads none and gets no row; a
// put replaces the list in its order, an empty list stays empty, and
// Delete forgets it.
func TestQuickReplies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewQuickReplies(db)
	me := accounttest.New(t, db)
	got, saved, err := s.Get(ctx, me)
	if err != nil || saved || got != nil {
		t.Fatalf("never saved: %q %v %v", got, saved, err)
	}
	var rows int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM quick_replies`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("a read wrote %d rows: %v", rows, err)
	}
	want := []string{"Mình ở Q.1, xem hàng buổi tối được", "Còn bạn ơi"}
	if err = s.Put(ctx, me, want); err != nil {
		t.Fatal(err)
	}
	if got, saved, err = s.Get(ctx, me); err != nil || !saved || !slices.Equal(got, want) {
		t.Fatalf("put then get: %q %v", got, err)
	}
	if err = s.Put(ctx, me, []string{}); err != nil {
		t.Fatal(err)
	}
	if got, saved, _ = s.Get(ctx, me); !saved || got == nil || len(got) != 0 {
		t.Fatalf("an empty list: %q", got)
	}
	if err = s.Delete(ctx, me); err != nil {
		t.Fatal(err)
	}
	if got, saved, _ = s.Get(ctx, me); saved {
		t.Fatalf("after delete: %q", got)
	}
}

// A put for an account deleted since the caller's checks writes nothing.
func TestQuickRepliesOfADeletedAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	gone := accounttest.New(t, db)
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewQuickReplies(db).Put(ctx, gone, []string{"Còn bạn ơi"}); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("put: %v", err)
	}
}

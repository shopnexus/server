package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// An account that never saved settings reads the defaults and gets no row;
// a put replaces the whole document, and Forget deletes it.
func TestSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewSettings(db)
	me := accounttest.New(t, db)
	got, err := s.Get(ctx, me)
	if err != nil || got != domain.DefaultSettings() {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	var rows int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM notification_settings`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("a read wrote %d rows: %v", rows, err)
	}
	want := domain.Settings{Offers: true, Reviews: true, Quiet: domain.Quiet{On: true, From: "23:30", To: "06:15"}}
	if err = s.Put(ctx, me, want); err != nil {
		t.Fatal(err)
	}
	if got, err = s.Get(ctx, me); err != nil || got != want {
		t.Fatalf("put then get: %+v %v", got, err)
	}
	want = domain.DefaultSettings()
	want.Followed = false
	if err = s.Put(ctx, me, want); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(ctx, me); got != want {
		t.Fatalf("a second put: %+v", got)
	}
	if err = repository.NewNotices(db).Forget(ctx, me); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(ctx, me); got != domain.DefaultSettings() {
		t.Fatalf("forgotten settings: %+v", got)
	}
}

// A put for an account deleted since the caller's checks writes nothing.
func TestSettingsOfADeletedAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	gone := accounttest.New(t, db)
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewSettings(db).Put(ctx, gone, domain.DefaultSettings()); !errors.Is(err, accountdomain.ErrSessionRevoked) {
		t.Fatalf("put: %v", err)
	}
}

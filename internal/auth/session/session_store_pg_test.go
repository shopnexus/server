package session_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// TestPGStore checks that the Postgres store behaves as session.Store
// promises. newStore also returns a function that creates an account to own
// rows.
func TestPGStore(t *testing.T) {
	t.Parallel()
	newStore := func(t *testing.T) (session.Store, func() uuid.UUID) {
		t.Helper()
		pool := pgtest.New(t)
		newAccount := func() uuid.UUID {
			id := uuid.NewV7()
			if _, err := pool.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`,
				pgx.StrictNamedArgs{"id": id}); err != nil {
				t.Fatal(err)
			}
			return id
		}
		return session.NewPGStore(pool), newAccount
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	create := func(t *testing.T, s session.Store, acc uuid.UUID, at time.Time) (session.Session, string) {
		t.Helper()
		sess, token := session.New(uuid.NewV7(), acc, "Pixel 9", at, at)
		if created, err := s.Create(ctx, sess); err != nil || !created {
			t.Fatalf("create: %v %v", created, err)
		}
		return sess, token
	}

	t.Run("create, get, find", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		acc := newAccount()
		sess, token := create(t, s, acc, now)
		if created, err := s.Create(ctx, sess); err != nil || created {
			t.Fatalf("second create: %v %v", created, err)
		}
		got, err := s.Get(ctx, sess.ID())
		if err != nil || got.AccountID() != acc || got.DeviceName() != "Pixel 9" || !got.AuthTime().Equal(now) ||
			!got.ExpiresAt().Equal(now.Add(session.MaxAge)) || got.WriteID() != sess.ID() || got.RevokedAt() != nil {
			t.Fatalf("get %+v %v", got, err)
		}
		if f, err := s.FindByRefresh(ctx, session.HashRefresh(token)); err != nil || f.ID() != sess.ID() {
			t.Fatalf("find: %v", err)
		}
		for _, find := range []func(context.Context, []byte) (session.Session, error){s.FindByRefresh, s.FindByPrevRefresh} {
			if _, err := find(ctx, []byte("nope")); !errors.Is(err, session.ErrNotFound) {
				t.Fatalf("unknown hash: %v", err)
			}
		}
		if _, err := s.Get(ctx, uuid.NewV7()); !errors.Is(err, session.ErrNotFound) {
			t.Fatalf("unknown id: %v", err)
		}
	})

	t.Run("rotate, conflict and retry", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		sess, token := create(t, s, newAccount(), now)
		stale := sess
		next := sess.Rotate(now.Add(time.Minute))
		retry := sess
		w := uuid.NewV7()
		if err := s.Save(ctx, &sess, w); err != nil {
			t.Fatal(err)
		}
		got, _ := s.FindByRefresh(ctx, session.HashRefresh(next))
		if !bytes.Equal(got.PrevRefreshHash(), session.HashRefresh(token)) || !got.LastUsedAt().Equal(now.Add(time.Minute)) {
			t.Fatalf("after rotate %+v", got)
		}
		if prev, err := s.FindByPrevRefresh(ctx, session.HashRefresh(token)); err != nil || prev.ID() != sess.ID() {
			t.Fatalf("by previous: %v", err)
		}
		stale.Rotate(now.Add(time.Minute))
		if err := s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, session.ErrConflict) {
			t.Fatalf("stale: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("revoke all", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		acc, other := newAccount(), newAccount()
		a, _ := create(t, s, acc, now)
		b, _ := create(t, s, acc, now)
		c, _ := create(t, s, other, now)
		if err := s.RevokeAll(ctx, acc, now); err != nil {
			t.Fatal(err)
		}
		for _, x := range []struct {
			id      uuid.UUID
			revoked bool
		}{{a.ID(), true}, {b.ID(), true}, {c.ID(), false}} {
			got, _ := s.Get(ctx, x.id)
			if (got.RevokedAt() != nil) != x.revoked {
				t.Fatalf("revoked %v, want %v", got.RevokedAt(), x.revoked)
			}
			if x.revoked && got.WriteID() == x.id {
				t.Fatal("revoke all left the write id, so a racing refresh would not conflict")
			}
		}
	})

	t.Run("list active", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newStore(t)
		acc, other := newAccount(), newAccount()
		first, _ := create(t, s, acc, now)
		revoked, _ := create(t, s, acc, now)
		revoked.Revoke(now)
		s.Save(ctx, &revoked, uuid.NewV7())
		create(t, s, acc, now.Add(-session.MaxAge-time.Hour))
		create(t, s, acc, now.Add(-session.IdleLimit-time.Hour))
		last, _ := create(t, s, acc, now)
		create(t, s, other, now)

		all, err := s.ListActive(ctx, acc, now, uuid.UUID{}, 10)
		if err != nil || len(all) != 2 || all[0].ID != last.ID() || all[1].ID != first.ID() || all[0].DeviceName != "Pixel 9" {
			t.Fatalf("list %+v %v", all, err)
		}
		page, _ := s.ListActive(ctx, acc, now, uuid.UUID{}, 1)
		next, _ := s.ListActive(ctx, acc, now, page[0].ID, 1)
		if len(page) != 1 || page[0].ID != last.ID() || len(next) != 1 || next[0].ID != first.ID() {
			t.Fatalf("pages %+v %+v", page, next)
		}
	})
}

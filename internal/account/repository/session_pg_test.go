package repository_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// TestPGSessions checks that the Postgres store behaves as repository.Sessions
// promises. newSessions also returns a function that creates an account to
// own rows.
func TestPGSessions(t *testing.T) {
	t.Parallel()
	newSessions := func(t *testing.T) (repository.Sessions, func() uuid.UUID) {
		t.Helper()
		pool := pgtest.New(t)
		return repository.NewSessions(pool), func() uuid.UUID { return accounttest.New(t, pool) }
	}
	ctx := context.Background()
	create := func(t *testing.T, s repository.Sessions, acc uuid.UUID, at time.Time) (domain.Session, string) {
		t.Helper()
		sess, token := domain.NewSession(uuid.NewV7(), acc, "Pixel 9", at, at)
		if stored, err := s.Create(ctx, sess, uuid.NewV7()); err != nil || stored != sess.ID() {
			t.Fatalf("create: %v %v", stored, err)
		}
		return sess, token
	}

	// Save leaves the session carrying the write ID it wrote, so saving it
	// again succeeds.
	t.Run("save twice", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		sess, _ := create(t, s, newAccount(), now)
		for n := range 2 {
			sess.Rotate(now.Add(time.Duration(n+1) * time.Minute))
			if err := s.Save(ctx, &sess, uuid.NewV7()); err != nil {
				t.Fatalf("save %d: %v", n, err)
			}
		}
	})

	t.Run("create, get, find", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		acc := newAccount()
		sess, token := create(t, s, acc, now)
		if stored, err := s.Create(ctx, sess, uuid.Nil()); err != nil || stored != sess.ID() {
			t.Fatalf("second create: %v %v", stored, err)
		}
		got, err := s.Get(ctx, sess.ID())
		if err != nil || got.AccountID() != acc || got.DeviceName() != "Pixel 9" || !got.AuthTime().Equal(now) ||
			!got.ExpiresAt().Equal(now.Add(domain.SessionMaxAge)) || got.WriteID() != sess.ID() || got.RevokedAt() != nil {
			t.Fatalf("get %+v %v", got, err)
		}
		if f, err := s.FindByRefresh(ctx, domain.HashRefresh(token)); err != nil || f.ID() != sess.ID() {
			t.Fatalf("find: %v", err)
		}
		for _, find := range []func(context.Context, []byte) (domain.Session, error){s.FindByRefresh, s.FindByPrevRefresh} {
			if _, err := find(ctx, []byte("nope")); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("unknown hash: %v", err)
			}
		}
		if _, err := s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("unknown id: %v", err)
		}
	})

	t.Run("rotate, conflict and retry", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		sess, token := create(t, s, newAccount(), now)
		stale := sess
		next := sess.Rotate(now.Add(time.Minute))
		retry := sess
		w := uuid.NewV7()
		if err := s.Save(ctx, &sess, w); err != nil {
			t.Fatal(err)
		}
		got, _ := s.FindByRefresh(ctx, domain.HashRefresh(next))
		if !bytes.Equal(got.PrevRefreshHash(), domain.HashRefresh(token)) || !got.LastUsedAt().Equal(now.Add(time.Minute)) {
			t.Fatalf("after rotate %+v", got)
		}
		if prev, err := s.FindByPrevRefresh(ctx, domain.HashRefresh(token)); err != nil || prev.ID() != sess.ID() {
			t.Fatalf("by previous: %v", err)
		}
		stale.Rotate(now.Add(time.Minute))
		if err := s.Save(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale: %v", err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("revoke others", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		acc, other := newAccount(), newAccount()
		a, _ := create(t, s, acc, now)
		b, _ := create(t, s, acc, now)
		kept, _ := create(t, s, acc, now)
		c, _ := create(t, s, other, now)
		if err := s.RevokeOthers(ctx, acc, kept.ID(), now, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		for _, x := range []struct {
			id      uuid.UUID
			revoked bool
		}{{a.ID(), true}, {b.ID(), true}, {kept.ID(), false}, {c.ID(), false}} {
			got, _ := s.Get(ctx, x.id)
			if (got.RevokedAt() != nil) != x.revoked {
				t.Fatalf("revoked %v, want %v", got.RevokedAt(), x.revoked)
			}
			if x.revoked && (got.WriteID() == x.id || got.RevokeReason() != domain.RevokedSignOut) {
				t.Fatalf("write id %s, reason %q: a racing refresh would not conflict, or the reason is lost", got.WriteID(), got.RevokeReason())
			}
		}
	})

	// Reauthenticate finds no revoked session, moves auth_time forward only
	// and keeps the write ID, so a refresh read before it saves, and the
	// save hands the refresh the new auth_time to sign with.
	t.Run("reauthenticate", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		sess, _ := create(t, s, newAccount(), now)
		later := now.Add(20 * time.Minute)
		got, err := s.Reauthenticate(ctx, sess.ID(), later)
		if err != nil || !got.AuthTime().Equal(later) || got.WriteID() != sess.WriteID() || got.ID() != sess.ID() {
			t.Fatalf("%+v %v", got, err)
		}
		if got, err = s.Reauthenticate(ctx, sess.ID(), now); err != nil || !got.AuthTime().Equal(later) {
			t.Fatalf("an earlier time moved auth_time back: %v %v", got.AuthTime(), err)
		}
		sess.Rotate(later)
		if err = s.Save(ctx, &sess, uuid.NewV7()); err != nil {
			t.Fatalf("a refresh read before the reauthentication: %v", err)
		}
		if !sess.AuthTime().Equal(later) {
			t.Fatalf("the refresh signs with auth_time %v", sess.AuthTime())
		}
		if got, _ = s.Get(ctx, sess.ID()); !got.AuthTime().Equal(later) {
			t.Fatalf("the refresh wrote auth_time back: %v", got.AuthTime())
		}
		sess.Revoke(domain.RevokedSignOut, later)
		if err = s.Save(ctx, &sess, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Reauthenticate(ctx, sess.ID(), later.Add(time.Minute)); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("a revoked session: %v", err)
		}
		if _, err = s.Reauthenticate(ctx, uuid.NewV7(), later); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("an unknown session: %v", err)
		}
	})

	// A session past its age or idle limit at the time given is not found,
	// and its auth_time stays.
	t.Run("reauthenticate an ended session", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		acc := newAccount()
		old, _ := create(t, s, acc, now)
		if _, err := s.Reauthenticate(ctx, old.ID(), old.ExpiresAt()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("at its expiry: %v", err)
		}
		idle, _ := create(t, s, acc, now)
		if _, err := s.Reauthenticate(ctx, idle.ID(), now.Add(domain.SessionIdleLimit)); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("idle: %v", err)
		}
		for _, x := range []domain.Session{old, idle} {
			if got, _ := s.Get(ctx, x.ID()); !got.AuthTime().Equal(now) {
				t.Fatalf("auth_time %v", got.AuthTime())
			}
		}
		if _, err := s.Reauthenticate(ctx, idle.ID(), now.Add(domain.SessionIdleLimit-time.Second)); err != nil {
			t.Fatalf("a second before the idle limit: %v", err)
		}
	})

	t.Run("revoke reason", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		x, _ := create(t, s, newAccount(), now)
		x.Revoke(domain.RevokedSuspended, now)
		if err := s.Save(ctx, &x, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Get(ctx, x.ID())
		if got.RevokedAt() == nil || got.RevokeReason() != domain.RevokedSuspended {
			t.Fatalf("revoked %v, reason %q", got.RevokedAt(), got.RevokeReason())
		}
	})

	t.Run("list active", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		acc, other := newAccount(), newAccount()
		first, _ := create(t, s, acc, now)
		revoked, _ := create(t, s, acc, now)
		revoked.Revoke(domain.RevokedSignOut, now)
		s.Save(ctx, &revoked, uuid.NewV7())
		create(t, s, acc, now.Add(-domain.SessionMaxAge-time.Hour))
		create(t, s, acc, now.Add(-domain.SessionIdleLimit-time.Hour))
		last, _ := create(t, s, acc, now)
		create(t, s, other, now)

		all, err := s.ListActive(ctx, acc, now, page.Request{Limit: 10})
		if err != nil || len(all) != 2 || all[0].ID != last.ID() || all[1].ID != first.ID() || all[0].DeviceName != "Pixel 9" {
			t.Fatalf("list %+v %v", all, err)
		}
		// A page of one reads two rows: the page and the row that says more follow.
		one, _ := s.ListActive(ctx, acc, now, page.Request{Limit: 1})
		next, _ := s.ListActive(ctx, acc, now, page.Request{Cursor: one[0].ID, Limit: 1})
		if len(one) != 2 || one[0].ID != last.ID() || len(next) != 1 || next[0].ID != first.ID() {
			t.Fatalf("pages %+v %+v", one, next)
		}
	})

	// A session idle past domain.SessionIdleLimit gets no pushes, as it
	// cannot refresh either.
	t.Run("active among", func(t *testing.T) {
		t.Parallel()
		s, newAccount := newSessions(t)
		acc := newAccount()
		live, _ := create(t, s, acc, now)
		revoked, _ := create(t, s, acc, now)
		revoked.Revoke(domain.RevokedSignOut, now)
		if err := s.Save(ctx, &revoked, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		expired, _ := create(t, s, acc, now.Add(-domain.SessionMaxAge-time.Hour))
		idle, _ := create(t, s, acc, now.Add(-domain.SessionIdleLimit-time.Hour))
		got, err := s.ActiveAmong(ctx, []uuid.UUID{live.ID(), revoked.ID(), expired.ID(), idle.ID(), uuid.NewV7()}, now)
		if err != nil || len(got) != 1 || !got[live.ID()] {
			t.Fatalf("%v %v", got, err)
		}
	})
}

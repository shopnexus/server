package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// devices issues sessions for acc and returns the principal of each.
func devices(t *testing.T, s repository.Sessions, acc uuid.UUID, n int, at time.Time) []authn.Principal {
	t.Helper()
	out := make([]authn.Principal, 0, n)
	for range n {
		id := uuid.NewV7()
		if _, err := authWith(s, nil, at, discard).IssueSession(context.Background(), id, acc, "Pixel 9"); err != nil {
			t.Fatal(err)
		}
		out = append(out, authn.Principal{AccountID: acc, SessionID: id, AuthTime: at})
	}
	return out
}

func revokedIn(t *testing.T, s repository.Sessions, id uuid.UUID) bool {
	t.Helper()
	got, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.RevokedAt() != nil
}

func TestLogoutRevokesOnlyThisSession(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	ps := devices(t, s, newAccount(t, db), 2, now)
	if err := authWith(s, nil, now, discard).Logout(context.Background(), ps[1]); err != nil {
		t.Fatal(err)
	}
	if !revokedIn(t, s, ps[1].SessionID) || revokedIn(t, s, ps[0].SessionID) {
		t.Fatal("wrong session revoked")
	}
}

func TestLogoutAllNeedsRecentSignIn(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	ps := devices(t, s, newAccount(t, db), 2, now)
	other := devices(t, s, newAccount(t, db), 1, now)
	if err := authWith(s, nil, now.Add(15*time.Minute+time.Second), discard).LogoutAll(context.Background(), ps[0]); !errors.Is(err, domain.ErrReauthRequired) {
		t.Fatalf("stale sign-in: %v", err)
	}
	if err := authWith(s, nil, now.Add(14*time.Minute), discard).LogoutAll(context.Background(), ps[0]); err != nil {
		t.Fatal(err)
	}
	if !revokedIn(t, s, ps[0].SessionID) || !revokedIn(t, s, ps[1].SessionID) || revokedIn(t, s, other[0].SessionID) {
		t.Fatal("wrong sessions revoked")
	}
}

func TestRemoveSessionChecksTheOwner(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	mine := devices(t, s, newAccount(t, db), 2, now)
	theirs := devices(t, s, newAccount(t, db), 1, now)
	if err := authWith(s, nil, now, discard).RemoveSession(context.Background(), mine[0], theirs[0].SessionID); !errors.Is(err, domain.ErrSessionNotFound) || revokedIn(t, s, theirs[0].SessionID) {
		t.Fatalf("another account's session: %v", err)
	}
	if err := authWith(s, nil, now, discard).RemoveSession(context.Background(), mine[0], mine[1].SessionID); err != nil || !revokedIn(t, s, mine[1].SessionID) {
		t.Fatalf("own session: %v", err)
	}
	if err := authWith(s, nil, now, discard).RemoveSession(context.Background(), mine[0], uuid.NewV7()); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("unknown session: %v", err)
	}
}

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/pgtest"
)

// devices issues sessions for acc and returns the principal of each.
func devices(t *testing.T, s session.Store, acc uuid.UUID, n int, at time.Time) []authn.Principal {
	t.Helper()
	var out []authn.Principal
	for range n {
		id := uuid.NewV7()
		if _, err := auth.IssueSession(context.Background(), s, session.NewSigner(testKeys), id, acc, "Pixel 9", at); err != nil {
			t.Fatal(err)
		}
		out = append(out, authn.Principal{AccountID: acc, SessionID: id, AuthTime: at})
	}
	return out
}

func revokedIn(t *testing.T, s session.Store, id uuid.UUID) bool {
	t.Helper()
	got, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.RevokedAt() != nil
}

func TestLogoutRevokesOnlyThisSession(t *testing.T) {
	db := pgtest.New(t)
	s := session.NewPGStore(db)
	ps := devices(t, s, newAccount(t, db), 2, now)
	if err := auth.Logout(context.Background(), s, ps[1], now); err != nil {
		t.Fatal(err)
	}
	if !revokedIn(t, s, ps[1].SessionID) || revokedIn(t, s, ps[0].SessionID) {
		t.Fatal("wrong session revoked")
	}
}

func TestLogoutAllNeedsRecentSignIn(t *testing.T) {
	db := pgtest.New(t)
	s := session.NewPGStore(db)
	ps := devices(t, s, newAccount(t, db), 2, now)
	other := devices(t, s, newAccount(t, db), 1, now)
	if err := auth.LogoutAll(context.Background(), s, ps[0], now.Add(15*time.Minute+time.Second)); !errors.Is(err, auth.ErrReauthRequired) {
		t.Fatalf("stale sign-in: %v", err)
	}
	if err := auth.LogoutAll(context.Background(), s, ps[0], now.Add(14*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !revokedIn(t, s, ps[0].SessionID) || !revokedIn(t, s, ps[1].SessionID) || revokedIn(t, s, other[0].SessionID) {
		t.Fatal("wrong sessions revoked")
	}
}

func TestRemoveSessionChecksTheOwner(t *testing.T) {
	db := pgtest.New(t)
	s := session.NewPGStore(db)
	mine := devices(t, s, newAccount(t, db), 2, now)
	theirs := devices(t, s, newAccount(t, db), 1, now)
	if err := auth.RemoveSession(context.Background(), s, mine[0], theirs[0].SessionID, now); !errors.Is(err, auth.ErrSessionNotFound) || revokedIn(t, s, theirs[0].SessionID) {
		t.Fatalf("another account's session: %v", err)
	}
	if err := auth.RemoveSession(context.Background(), s, mine[0], mine[1].SessionID, now); err != nil || !revokedIn(t, s, mine[1].SessionID) {
		t.Fatalf("own session: %v", err)
	}
	if err := auth.RemoveSession(context.Background(), s, mine[0], uuid.NewV7(), now); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("unknown session: %v", err)
	}
}

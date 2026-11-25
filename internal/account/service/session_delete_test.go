package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// devices issues sessions for acc and returns the principal of each.
func devices(t *testing.T, s repository.Sessions, acc uuid.UUID, n int, when time.Time) []authn.Principal {
	t.Helper()
	out := make([]authn.Principal, 0, n)
	for range n {
		id := uuid.NewV7()
		if _, err := service.IssueSession(at(nil, s, when), context.Background(), id, acc, uuid.Nil(), "Pixel 9"); err != nil {
			t.Fatal(err)
		}
		out = append(out, authn.Principal{AccountID: acc, SessionID: id, AuthTime: when})
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

func TestDeleteCurrentSessionRevokesOnlyIt(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	ps := devices(t, s, accounttest.New(t, db), 2, now)
	if err := at(nil, s, now).DeleteCurrentSession(context.Background(), ps[1]); err != nil {
		t.Fatal(err)
	}
	if !revokedIn(t, s, ps[1].SessionID) || revokedIn(t, s, ps[0].SessionID) {
		t.Fatal("wrong session revoked")
	}
}

// Signing out the other devices keeps the caller's session and leaves
// other accounts alone; it needs a recent sign-in.
func TestDeleteOtherSessions(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	ps := devices(t, s, accounttest.New(t, db), 3, now)
	other := devices(t, s, accounttest.New(t, db), 1, now)
	if err := at(nil, s, now.Add(15*time.Minute+time.Second)).DeleteOtherSessions(context.Background(), ps[1]); !errors.Is(err, domain.ErrReauthRequired) {
		t.Fatalf("stale sign-in: %v", err)
	}
	if revokedIn(t, s, ps[0].SessionID) {
		t.Fatal("a refused sign-out revoked a session")
	}
	if err := at(nil, s, now.Add(14*time.Minute)).DeleteOtherSessions(context.Background(), ps[1]); err != nil {
		t.Fatal(err)
	}
	if revokedIn(t, s, ps[1].SessionID) || !revokedIn(t, s, ps[0].SessionID) || !revokedIn(t, s, ps[2].SessionID) ||
		revokedIn(t, s, other[0].SessionID) {
		t.Fatal("wrong sessions revoked")
	}
	if got, _ := s.Get(context.Background(), ps[0].SessionID); got.RevokeReason() != domain.RevokedSignOut {
		t.Fatalf("reason %q", got.RevokeReason())
	}
}

func TestDeleteSessionChecksTheOwner(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	mine := devices(t, s, accounttest.New(t, db), 2, now)
	theirs := devices(t, s, accounttest.New(t, db), 1, now)
	if err := at(nil, s, now).DeleteSession(context.Background(), mine[0], theirs[0].SessionID); !errors.Is(err, domain.ErrSessionNotFound) || revokedIn(t, s, theirs[0].SessionID) {
		t.Fatalf("another account's session: %v", err)
	}
	if err := at(nil, s, now).DeleteSession(context.Background(), mine[0], mine[1].SessionID); err != nil || !revokedIn(t, s, mine[1].SessionID) {
		t.Fatalf("own session: %v", err)
	}
	if err := at(nil, s, now).DeleteSession(context.Background(), mine[0], uuid.NewV7()); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("unknown session: %v", err)
	}
}

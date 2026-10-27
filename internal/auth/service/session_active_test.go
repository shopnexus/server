package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// Only a session neither revoked nor expired is active.
func TestActiveSessions(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewSessions(db)
	ps := devices(t, s, newAccount(t, db), 2, now)
	old := devices(t, s, newAccount(t, db), 1, now.Add(-365*24*time.Hour))
	a := authWith(s, nil, now, discard)
	if err := a.Logout(context.Background(), ps[1]); err != nil {
		t.Fatal(err)
	}
	got, err := a.ActiveSessions(context.Background(), []uuid.UUID{ps[0].SessionID, ps[1].SessionID, old[0].SessionID})
	if err != nil || !got[ps[0].SessionID] || got[ps[1].SessionID] || got[old[0].SessionID] {
		t.Fatalf("%v %v", got, err)
	}
}

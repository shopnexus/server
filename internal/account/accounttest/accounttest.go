// Package accounttest creates accounts in a test database for the tests of
// modules that need someone to act.
package accounttest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/contact"
)

// New creates an active account named Linh with no sign-in method and
// returns its ID.
func New(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	a := account(t)
	if err := repository.NewAccounts(db).Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID()
}

// WithPhone creates an active account named Linh that signs in with a
// random phone number, as a seller needs, and returns its ID.
func WithPhone(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	phone, err := contact.ParsePhone(fmt.Sprintf("09%08d", rand.IntN(100_000_000))) //nolint:gosec // A test number needs no secrecy.
	if err != nil {
		t.Fatal(err)
	}
	a := account(t)
	now := time.Now()
	c := domain.NewCredentials(a.ID(), domain.NewIdentity(uuid.NewV7(), phone, now), now)
	if err := repository.NewCredentials(db).SignUp(context.Background(), a, c); err != nil {
		t.Fatal(err)
	}
	return a.ID()
}

func account(t *testing.T) domain.Account {
	t.Helper()
	a, err := domain.New(uuid.NewV7(), "Linh", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

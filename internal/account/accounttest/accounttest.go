// Package accounttest creates accounts in a test database and builds the
// account service for the tests of modules that need someone to act.
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
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/audit"
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

// Service is the account service on db with the accounts, follows,
// addresses and credentials stores, as the modules that call it need.
func Service(db *pgxpool.Pool) *service.Account {
	return service.New(service.Deps{
		Accounts: repository.NewAccounts(db), Follows: repository.NewFollows(db), Addresses: repository.NewAddresses(db),
		Credentials: repository.NewCredentials(db), Now: time.Now,
	})
}

// Suspend suspends account id for spam, as the CLI does.
func Suspend(t *testing.T, accounts service.Service, id uuid.UUID) {
	t.Helper()
	if _, err := accounts.Suspend(context.Background(), service.SuspendInput{ID: id, Reason: "spam", By: audit.ByCLI("test")}); err != nil {
		t.Fatal(err)
	}
}

func account(t *testing.T) domain.Account {
	t.Helper()
	a, err := domain.New(uuid.NewV7(), "Linh", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

package service_test

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/contact"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

var (
	now      = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	testKeys = func() config.AuthConfig {
		k := config.NewAuthKeys()
		k.Issuer, k.Audience = "server-test", "shopnexus-app-test"
		return k
	}()
	discard = slog.New(slog.DiscardHandler)
)

func newStore(t *testing.T) repository.Accounts {
	t.Helper()
	return repository.NewAccounts(pgtest.New(t))
}

// at is the service on accounts and sessions, either of which may be nil,
// with its clock stopped at t. It signs with testKeys and logs nowhere.
func at(accounts repository.Accounts, sessions repository.Sessions, t time.Time) *service.Account {
	return service.New(service.Deps{
		Accounts: accounts, Sessions: sessions, Signer: domain.NewSigner(testKeys), Now: func() time.Time { return t }, Log: discard,
	})
}

// seed creates an active account named Linh in s.
func seed(t *testing.T, s repository.Accounts) uuid.UUID {
	t.Helper()
	a, err := domain.New(uuid.NewV7(), "Linh", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID()
}

// suspendAccount marks an account suspended, as an admin would.
func suspendAccount(t *testing.T, db *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		`UPDATE accounts SET status = 'suspended', suspend_reason = 'test' WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
}

func phone(t *testing.T, raw string) contact.Address {
	t.Helper()
	a, err := contact.ParsePhone(raw)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func email(t *testing.T, raw string) contact.Address {
	t.Helper()
	a, err := contact.ParseEmail(raw)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

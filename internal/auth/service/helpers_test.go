package service_test

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/auth/domain"
	"github.com/shopnexus/server2/internal/auth/repository"
	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/contact"
)

var (
	now      = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	testKeys = func() config.AuthConfig {
		k := config.NewAuthKeys()
		k.Issuer, k.Audience = "server2-test", "shopnexus-app-test"
		return k
	}()
	discard = slog.New(slog.DiscardHandler)
)

// accountsOn is module account's adapter on db, reading statuses at now.
func accountsOn(db *pgxpool.Pool) accountsvc.Service {
	return accountsvc.New(accountrepo.NewAccounts(db), nil, func() time.Time { return now })
}

// newAccount creates an active account in db and returns its ID.
func newAccount(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := accountsOn(db).Create(context.Background(), id, ""); err != nil {
		t.Fatal(err)
	}
	return id
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

// authWith is Auth on sessions and accounts, signing with testKeys, its log
// on log and its clock stopped at the given time.
func authWith(sessions repository.Sessions, accounts accountsvc.Service, at time.Time, log *slog.Logger) *service.Auth {
	return service.New(service.Deps{
		Sessions: sessions, Signer: domain.NewSigner(testKeys), Accounts: accounts, Now: func() time.Time { return at }, Log: log,
	})
}

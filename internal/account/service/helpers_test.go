package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

func newStore(t *testing.T) repository.Accounts {
	t.Helper()
	return repository.NewAccounts(pgtest.New(t))
}

// at is the service on s with its clock stopped at t.
func at(s repository.Accounts, t time.Time) *service.Account {
	return service.New(s, func() time.Time { return t })
}

// seed creates an active account named Linh.
func seed(t *testing.T, s repository.Accounts) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := at(s, now).Create(context.Background(), id, "Linh"); err != nil {
		t.Fatal(err)
	}
	return id
}

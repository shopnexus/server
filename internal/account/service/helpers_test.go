package service_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

func newStore(t *testing.T) repository.Accounts {
	t.Helper()
	return repository.NewAccounts(pgtest.New(t))
}

// at is the service on s with its clock stopped at t.
func at(s repository.Accounts, t time.Time) *service.Account {
	return service.New(s, nil, func() time.Time { return t })
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

// kindsOf lists the kinds of the events about subject, oldest first.
func kindsOf(t *testing.T, db *pgxpool.Pool, subject uuid.UUID) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT kind FROM events WHERE subject_id = @id ORDER BY id`, pgx.StrictNamedArgs{"id": subject})
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return kinds
}

// Update, Suspend and Reinstate each announce their change.
func TestUseCasesPublish(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	ctx := context.Background()
	id := seed(t, s)
	if _, err := at(s, now).Update(ctx, id, service.UpdateInput{DisplayName: new("Minh")}); err != nil {
		t.Fatal(err)
	}
	if _, err := at(s, now).Suspend(ctx, service.SuspendInput{ID: id, Reason: "spam", By: audit.ByCLI("test")}); err != nil {
		t.Fatal(err)
	}
	if _, err := at(s, now).Reinstate(ctx, id, audit.ByCLI("test")); err != nil {
		t.Fatal(err)
	}
	if got := kindsOf(t, db, id); !slices.Equal(got, []string{"account.updated", "account.suspended", "account.unsuspended"}) {
		t.Fatalf("%v", got)
	}
}

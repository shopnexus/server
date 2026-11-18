package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

func TestErase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts := repository.NewAccounts(db)
	svc := at(accounts, nil, now)
	id := seed(t, accounts)

	if err := svc.Erase(ctx, id); err != nil {
		t.Fatal(err)
	}
	st, err := svc.Status(ctx, id)
	if err != nil || st != domain.StatusDeleted {
		t.Fatalf("status: %s %v", st, err)
	}
	if err = svc.Erase(ctx, id); err != nil {
		t.Fatalf("erase again: %v", err)
	}
	if kinds := pgtest.EventKinds(t, db, id); len(kinds) != 1 || kinds[0] != "account.deleted" {
		t.Fatalf("events: %v", kinds)
	}
	v, err := svc.Get(ctx, id)
	if err != nil || v.Status != domain.StatusDeleted || v.DisplayName != "" {
		t.Fatalf("view: %+v %v", v, err)
	}
}

func TestEraseRefusesASuspension(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	accounts := repository.NewAccounts(db)
	id := seed(t, accounts)
	suspendAccount(t, db, id)
	if err := at(accounts, nil, now).Erase(context.Background(), id); !errors.Is(err, domain.ErrSuspended) {
		t.Fatalf("erase while suspended: %v", err)
	}
}

// conflictOnce is an account store whose first Erase loses a race.
type conflictOnce struct {
	repository.Accounts

	failed *bool
}

func (c conflictOnce) Erase(ctx context.Context, a *domain.Account, writeID uuid.UUID, evs ...events.Event) error {
	if !*c.failed {
		*c.failed = true
		return repository.ErrConflict
	}
	return c.Accounts.Erase(ctx, a, writeID, evs...)
}

// alwaysConflict is an account store whose every Erase loses a race.
type alwaysConflict struct{ repository.Accounts }

func (alwaysConflict) Erase(context.Context, *domain.Account, uuid.UUID, ...events.Event) error {
	return repository.ErrConflict
}

func TestEraseRetriesAConflictOnce(t *testing.T) {
	t.Parallel()
	accounts := repository.NewAccounts(pgtest.New(t))
	id := seed(t, accounts)
	failed := false
	if err := at(conflictOnce{Accounts: accounts, failed: &failed}, nil, now).Erase(context.Background(), id); err != nil {
		t.Fatalf("erase after one conflict: %v", err)
	}
	other := seed(t, accounts)
	if err := at(alwaysConflict{Accounts: accounts}, nil, now).Erase(context.Background(), other); !errors.Is(err, apperr.ErrTryAgain) {
		t.Fatalf("erase after two conflicts: %v", err)
	}
}

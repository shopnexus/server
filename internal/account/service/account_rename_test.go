package service_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

func TestRename(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if v, err := at(s, now).Rename(context.Background(), id, " Minh "); err != nil || v.DisplayName != "Minh" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestRenameRefusedWhileSuspended(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	a, _ := s.Get(context.Background(), id)
	a.Suspend("spam", nil, now)
	s.Save(context.Background(), &a, uuid.NewV7())
	if _, err := at(s, now).Rename(context.Background(), id, "Minh"); !errors.Is(err, domain.ErrSuspended) {
		t.Fatalf("err %v", err)
	}
}

// conflicting is a store whose Save always loses a race.
type conflicting struct{ repository.Accounts }

func (conflicting) Save(context.Context, *domain.Account, uuid.UUID) error {
	return repository.ErrConflict
}

// A lost race reaches the client as try_again, the code auth uses too.
func TestRenameReportsAConflict(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if _, err := at(conflicting{s}, now).Rename(context.Background(), id, "Minh"); !errors.Is(err, apperr.ErrTryAgain) {
		t.Fatalf("err %v", err)
	}
}

package account_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/apperr"
)

func TestRename(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if v, err := account.Rename(context.Background(), s, id, " Minh ", now); err != nil || v.DisplayName != "Minh" {
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
	if _, err := account.Rename(context.Background(), s, id, "Minh", now); !errors.Is(err, account.ErrSuspended) {
		t.Fatalf("err %v", err)
	}
}

// conflicting is a store whose Save always loses a race.
type conflicting struct{ account.Store }

func (conflicting) Save(context.Context, *account.Account, uuid.UUID) error {
	return account.ErrConflict
}

// A lost race reaches the client as try_again, the code auth uses too.
func TestRenameReportsAConflict(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	id := seed(t, s)
	if _, err := account.Rename(context.Background(), conflicting{s}, id, "Minh", now); !errors.Is(err, apperr.ErrTryAgain) {
		t.Fatalf("err %v", err)
	}
}

package account_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
)

func seed(t *testing.T, s account.Store) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := account.Create(context.Background(), s, id, "Linh", now); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateIsIdempotent(t *testing.T) {
	s := accounttest.NewMemStore()
	id := seed(t, s)
	if err := account.Create(context.Background(), s, id, "Linh", now); err != nil {
		t.Fatal(err)
	}
	if v, err := account.Get(context.Background(), s, id, now); err != nil || v.DisplayName != "Linh" || v.Status != account.StatusActive {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestCreateRejectsLongName(t *testing.T) {
	err := account.Create(context.Background(), accounttest.NewMemStore(), uuid.NewV7(), strings.Repeat("a", 51), now)
	if !errors.Is(err, account.ErrInvalidName) {
		t.Fatalf("err %v", err)
	}
}

func TestStatusFollowsTheClock(t *testing.T) {
	s := accounttest.NewMemStore()
	id := seed(t, s)
	a, _ := s.Get(context.Background(), id)
	until := now.Add(time.Hour)
	if err := a.Suspend("spam", &until, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(context.Background(), &a, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		at   time.Time
		want account.Status
	}{{now, account.StatusSuspended}, {until.Add(time.Second), account.StatusActive}} {
		if st, err := account.StatusOf(context.Background(), s, id, c.at); err != nil || st != c.want {
			t.Fatalf("at %s: %s %v", c.at, st, err)
		}
	}
}

func TestRename(t *testing.T) {
	s := accounttest.NewMemStore()
	id := seed(t, s)
	if v, err := account.Rename(context.Background(), s, id, " Minh ", now); err != nil || v.DisplayName != "Minh" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestRenameRefusedWhileSuspended(t *testing.T) {
	s := accounttest.NewMemStore()
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

func TestRenameReportsAConflict(t *testing.T) {
	s := accounttest.NewMemStore()
	id := seed(t, s)
	if _, err := account.Rename(context.Background(), conflicting{s}, id, "Minh", now); !errors.Is(err, account.ErrConflict) {
		t.Fatalf("err %v", err)
	}
}

func TestClientDelegates(t *testing.T) {
	s := accounttest.NewMemStore()
	c := account.Client{Store: s, Now: func() time.Time { return now }}
	id := uuid.NewV7()
	if err := c.Create(context.Background(), id, ""); err != nil {
		t.Fatal(err)
	}
	if st, err := c.Status(context.Background(), id); err != nil || st != account.StatusActive {
		t.Fatalf("%s %v", st, err)
	}
	if err := c.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(context.Background(), id); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

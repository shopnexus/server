package account_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
)

func TestStatusUnknown(t *testing.T) {
	uc := account.AccountStatus{Store: accounttest.NewMemStore(), Now: clock(now)}
	if _, err := uc.Execute(context.Background(), nil, uuid.NewV7()); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestStatusUsesTheClock(t *testing.T) {
	store := accounttest.NewMemStore()
	a := seed(t, store)
	until := now.Add(time.Hour)
	if err := a.Suspend("spam", &until, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), nil, a); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		at   time.Time
		want account.Status
	}{
		{now, account.StatusSuspended},
		{until.Add(time.Second), account.StatusActive},
	} {
		uc := account.AccountStatus{Store: store, Now: clock(tc.at)}
		st, err := uc.Execute(context.Background(), nil, a.ID())
		if err != nil || st != tc.want {
			t.Fatalf("at %s: %s %v, want %s", tc.at, st, err, tc.want)
		}
	}
}

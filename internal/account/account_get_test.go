package account_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
)

func TestGetReturnsView(t *testing.T) {
	store := accounttest.NewMemStore()
	a := seed(t, store)
	uc := account.AccountGet{Store: store, Now: clock(now)}

	got, err := uc.Execute(context.Background(), a.ID())
	if err != nil || got.ID != a.ID() || got.DisplayName != "Linh" || got.Status != account.StatusActive {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestGetUnknown(t *testing.T) {
	uc := account.AccountGet{Store: accounttest.NewMemStore(), Now: clock(now)}
	if _, err := uc.Execute(context.Background(), uuid.NewV7()); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}

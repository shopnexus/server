package account_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
)

func TestCreateStoresActiveAccount(t *testing.T) {
	store := accounttest.NewMemStore()
	uc := account.AccountCreate{Store: store, Now: clock(now)}

	id, err := uc.Execute(context.Background(), nil, "Linh")
	if err != nil {
		t.Fatal(err)
	}
	a, err := store.Get(context.Background(), nil, id)
	if err != nil || a.DisplayName() != "Linh" || a.Status(now) != account.StatusActive {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestCreateRejectsLongName(t *testing.T) {
	uc := account.AccountCreate{Store: accounttest.NewMemStore(), Now: clock(now)}
	if _, err := uc.Execute(context.Background(), nil, strings.Repeat("a", 51)); !errors.Is(err, account.ErrInvalidName) {
		t.Fatalf("err %v", err)
	}
}

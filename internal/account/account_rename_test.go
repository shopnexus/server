package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/account/accounttest"
)

func TestRename(t *testing.T) {
	store := accounttest.NewMemStore()
	a := seed(t, store)
	uc := account.AccountRename{Store: store, Tx: noTx{}, Now: clock(now)}

	got, err := uc.Execute(context.Background(), a.ID(), account.AccountRenameInput{DisplayName: " Minh "})
	if err != nil || got.DisplayName != "Minh" {
		t.Fatalf("%+v %v", got, err)
	}
	stored, _ := store.Get(context.Background(), nil, a.ID())
	if stored.DisplayName() != "Minh" {
		t.Fatalf("stored %q", stored.DisplayName())
	}
}

func TestRenameRefusedWhileSuspended(t *testing.T) {
	store := accounttest.NewMemStore()
	a := seed(t, store)
	if err := a.Suspend("spam", nil, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), nil, a); err != nil {
		t.Fatal(err)
	}
	uc := account.AccountRename{Store: store, Tx: noTx{}, Now: clock(now)}

	if _, err := uc.Execute(context.Background(), a.ID(), account.AccountRenameInput{DisplayName: "Minh"}); !errors.Is(err, account.ErrSuspended) {
		t.Fatalf("err %v", err)
	}
	stored, _ := store.Get(context.Background(), nil, a.ID())
	if stored.DisplayName() != "Linh" {
		t.Fatalf("name changed to %q", stored.DisplayName())
	}
}

func TestRenameRejectsEmptyName(t *testing.T) {
	store := accounttest.NewMemStore()
	a := seed(t, store)
	uc := account.AccountRename{Store: store, Tx: noTx{}, Now: clock(now)}
	if _, err := uc.Execute(context.Background(), a.ID(), account.AccountRenameInput{DisplayName: "  "}); !errors.Is(err, account.ErrInvalidName) {
		t.Fatalf("err %v", err)
	}
}

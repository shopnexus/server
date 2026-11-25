package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/shipping/repository"
)

func TestAccountsPutListAndDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts := repository.NewAccounts(db)
	owner, other := accounttest.New(t, db), accounttest.New(t, db)
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	row := func(c, shop string) repository.Row {
		return repository.Row{AccountID: owner, Carrier: c, ShopCode: shop, Token: []byte("sealed-" + shop), LinkedAt: at}
	}
	if _, err := accounts.Get(ctx, owner, "ghtk"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("nothing linked: %v", err)
	}
	for _, r := range []repository.Row{row("ghtk", "S1"), row("ghn", "885"), row("ghtk", "S2")} {
		if err := accounts.Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := accounts.Get(ctx, owner, "ghtk")
	if err != nil || got.ShopCode != "S2" || string(got.Token) != "sealed-S2" || !got.LinkedAt.Equal(at) {
		t.Fatalf("relinked: %+v %v", got, err)
	}
	list, err := accounts.List(ctx, owner)
	if err != nil || len(list) != 2 || list[0].Carrier != "ghn" || list[1].Carrier != "ghtk" {
		t.Fatalf("list %+v %v", list, err)
	}
	if list, _ = accounts.List(ctx, other); len(list) != 0 {
		t.Fatalf("another account's list %+v", list)
	}
	deleted, err := accounts.Delete(ctx, owner, "ghn")
	if err != nil || !deleted {
		t.Fatalf("delete %v %v", deleted, err)
	}
	if deleted, _ := accounts.Delete(ctx, owner, "ghn"); deleted {
		t.Fatal("deleted twice")
	}
	if err = accounts.DeleteAll(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if list, _ = accounts.List(ctx, owner); len(list) != 0 {
		t.Fatalf("after DeleteAll %+v", list)
	}
}

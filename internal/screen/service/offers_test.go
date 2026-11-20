package service_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/page"
)

func TestOffersShowTheSellerEachOfferWithItsBuyer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := accounttest.WithPhone(t, e.db), accounttest.WithPhone(t, e.db)
	d, _, err := e.deals.Open(ctx, buyer, catalogtest.Listing(t, e.db, seller))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.deals.Offer(ctx, buyer, d.ID, 1_000_000); err != nil {
		t.Fatal(err)
	}

	e.take()
	v, err := e.screen.Offers(ctx, seller, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.take(); got.many != 1 || got.single != 0 {
		t.Fatalf("the buyers took %+v reads, want one batch", got)
	}
	if len(v.Items) != 1 || v.NextCursor != nil || v.Items[0].Deal.ID != d.ID || v.Items[0].Deal.OpenPrice.Amount != 1_000_000 {
		t.Fatalf("seller's offers: %+v", v)
	}
	a, err := e.accounts.Get(ctx, buyer)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Items[0].Buyer; !reflect.DeepEqual(got, chatsvc.PersonFromAccount(buyer, a)) {
		t.Fatalf("buyer %+v", got)
	}
	// The buyer has no offers waiting on them.
	if mine, err := e.screen.Offers(ctx, buyer, page.Request{}); err != nil || len(mine.Items) != 0 {
		t.Fatalf("buyer's offers: %+v %v", mine, err)
	}
}

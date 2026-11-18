package carrier_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/provider/carrier"
)

func TestFakeLinksAnythingButWrong(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := carrier.Fake{Carrier: carrier.GHTK}
	l, err := f.Link(ctx, carrier.Credentials{Email: "an@shop.vn", Password: "x"})
	if err != nil || l.Token == "" || !strings.HasPrefix(l.ShopCode, "S") {
		t.Fatalf("%+v %v", l, err)
	}
	if again, _ := f.Link(ctx, carrier.Credentials{Email: "an@shop.vn", Password: "y"}); again.ShopCode != l.ShopCode {
		t.Fatalf("one email got %s and %s", l.ShopCode, again.ShopCode)
	}
	if _, err = f.Link(ctx, carrier.Credentials{Email: "an@shop.vn", Password: "wrong"}); !errors.Is(err, carrier.ErrLoginFailed) {
		t.Fatalf("wrong password: %v", err)
	}
	g := carrier.Fake{Carrier: carrier.GHN}
	if _, err = g.Link(ctx, carrier.Credentials{Token: "wrong", ShopID: "1"}); !errors.Is(err, carrier.ErrLoginFailed) {
		t.Fatalf("wrong token: %v", err)
	}
	if l, err = g.Link(ctx, carrier.Credentials{Token: "t", ShopID: "885"}); err != nil || l.ShopCode != "885" {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestFakeReadsEveryCodeButNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := carrier.Fake{Carrier: carrier.GHN}
	p, err := f.Lookup(ctx, carrier.Account{}, "GHN0123")
	if err != nil || p.Status != carrier.StatusPendingPickup || p.Fee == nil || *p.Fee != 22_000 || p.COD != 1_500_000 ||
		p.RecipientName == "" || p.District == "" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, code := range []string{"NOTFOUND", "NOTFOUND-42"} {
		if _, err = f.Lookup(ctx, carrier.Account{}, code); !errors.Is(err, carrier.ErrNotFound) {
			t.Errorf("%s: %v", code, err)
		}
	}
}

func TestFakeMakesOneCodePerOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := carrier.Fake{Carrier: carrier.GHN}
	in := carrier.CreateInput{OrderCode: "SN-100001", COD: 1_500_000, WeightGrams: 800, Service: "standard", Option: "home"}
	a, err := f.Create(ctx, carrier.Account{}, in)
	if err != nil || !strings.HasPrefix(a.TrackingCode, "GHN-") || a.Fee != 22_000 {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err = strconv.ParseUint(strings.TrimPrefix(a.TrackingCode, "GHN-"), 10, 64); err != nil {
		t.Fatalf("%s is not GHN- and a number", a.TrackingCode)
	}
	if b, _ := f.Create(ctx, carrier.Account{}, in); b != a {
		t.Fatalf("a retry made %+v after %+v", b, a)
	}
	in.OrderCode = "SN-100002"
	if c, _ := f.Create(ctx, carrier.Account{}, in); c.TrackingCode == a.TrackingCode {
		t.Fatalf("two orders got %s", a.TrackingCode)
	}
	if err = f.Cancel(ctx, carrier.Account{}, a.TrackingCode); err != nil {
		t.Fatal(err)
	}
}

func TestFakePickupsAndQuote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := carrier.Fake{Carrier: carrier.GHTK}
	if p, err := f.Pickups(ctx, carrier.Account{}); err != nil || len(p) != 2 || p[0].ID == "" {
		t.Fatalf("pickups %+v %v", p, err)
	}
	if fee, err := f.Quote(ctx, carrier.Account{}, carrier.QuoteInput{WeightGrams: 800}); err != nil || fee != 22_000 {
		t.Fatalf("fee %d %v", fee, err)
	}
}

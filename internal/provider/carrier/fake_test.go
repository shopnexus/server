package carrier_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/provider/carrier"
)

func TestFakeReadsEveryCodeButNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := carrier.Fake{}
	p, err := f.Lookup(ctx, "spx", "SPXVN0123")
	if err != nil || p.Status != carrier.StatusPendingPickup || p.Fee == nil || *p.Fee != 22_000 || p.COD != 1_500_000 ||
		p.RecipientName == "" || p.District == "" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, code := range []string{"NOTFOUND", "NOTFOUND-42"} {
		if _, err = f.Lookup(ctx, "ghn", code); !errors.Is(err, carrier.ErrNotFound) {
			t.Errorf("%s: %v", code, err)
		}
	}
	if _, err = f.Lookup(ctx, "ghn", "notfound"); err != nil {
		t.Errorf("a lower-case code is another code: %v", err)
	}
}

func TestFakeMakesOneCodePerOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := carrier.Fake{}
	in := carrier.CreateInput{OrderID: uuid.NewV7(), COD: 1_500_000, WeightGrams: 800, Service: "standard", Pickup: "home"}
	a, err := f.Create(ctx, in)
	if err != nil || !strings.HasPrefix(a.TrackingCode, "GHTK-") || a.Fee != 22_000 {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err = strconv.ParseUint(strings.TrimPrefix(a.TrackingCode, "GHTK-"), 10, 64); err != nil {
		t.Fatalf("%s is not GHTK- and a number", a.TrackingCode)
	}
	if b, _ := f.Create(ctx, in); b != a {
		t.Fatalf("a retry made %+v after %+v", b, a)
	}
	in.OrderID = uuid.NewV7()
	if c, _ := f.Create(ctx, in); c.TrackingCode == a.TrackingCode {
		t.Fatalf("two orders got %s", a.TrackingCode)
	}
	if err = f.Cancel(ctx, "ghtk", a.TrackingCode); err != nil {
		t.Fatal(err)
	}
}

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shopnexus/server/internal/seedpack"
)

// TestPackFitsTheScenario checks the demo pack offline and that it holds
// what the deals need, so a bad rebuild fails in go test before a run
// writes anything.
func TestPackFitsTheScenario(t *testing.T) {
	t.Parallel()
	p, err := seedpack.Open(filepath.Join("..", "..", filepath.FromSlash(packDir)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := seedpack.Verify(context.Background(), p, true, 4, seedpack.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pr := range r.Problems {
		t.Error(pr)
	}
	people, err := p.Accounts()
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]seedpack.Account{}
	for _, a := range people {
		byKey[a.Key] = a
		if a.Address == nil {
			t.Errorf("%s has no address; the COD order needs one", a.Key)
		}
	}
	listings, err := p.Listings()
	if err != nil {
		t.Fatal(err)
	}
	ls := map[string]seedpack.Listing{}
	for _, l := range listings {
		ls[l.Key] = l
	}
	for _, key := range []string{"u-linh", "u-kiet", "u-nam"} {
		if _, ok := byKey[key]; !ok {
			t.Errorf("the deals need %s", key)
		}
	}
	for _, key := range []string{"p-iphone13", "p-iphone12", "p-iphone14", "p-sofa", "p-a7"} {
		if _, ok := ls[key]; !ok {
			t.Errorf("the deals need %s", key)
		}
	}
	if l := ls["p-iphone13"]; l.Seller != "u-kiet" || !l.AllowOffers || 4_800_000 >= l.Price || 4_800_000*2 < l.Price {
		t.Errorf("Linh's offer of 4,800,000 does not fit p-iphone13: %+v", l)
	}
	if l := ls["p-iphone12"]; l.Seller != "u-nam" || l.Price != 6_900_000 {
		t.Errorf("Linh accepts p-iphone12 at 6,900,000: %+v", l)
	}
}

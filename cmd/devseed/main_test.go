package main

import (
	"testing"
	"unicode/utf8"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/contact"
)

// TestSeedFitsTheAPI checks data/seed.json against the rules POST
// /v1/listings and the deals apply, so a bad edit fails in go test before
// a run posts anything.
func TestSeedFitsTheAPI(t *testing.T) {
	t.Parallel()
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	people := map[string]person{}
	for _, p := range s.Accounts {
		if _, dup := people[p.Key]; dup {
			t.Errorf("person %s twice", p.Key)
		}
		people[p.Key] = p
		if _, err := contact.ParsePhone(p.Phone); err != nil {
			t.Errorf("%s: phone %q: %v", p.Key, p.Phone, err)
		}
		if p.Kind != "home" && p.Kind != "work" && p.Kind != "other" {
			t.Errorf("%s: address kind %q", p.Key, p.Kind)
		}
		if p.Avatar != "" {
			if _, err := data.ReadFile("data/photos/" + p.Avatar); err != nil {
				t.Errorf("%s: avatar: %v", p.Key, err)
			}
		}
	}
	onSale := map[string]int{}
	listings := map[string]listing{}
	for _, l := range s.Listings {
		listings[l.Key] = l
		if _, ok := people[l.Seller]; !ok {
			t.Errorf("%s: no seller %s", l.Key, l.Seller)
		}
		onSale[l.Seller]++
		if n := utf8.RuneCountInString(l.Title); n < 1 || n > catalogdomain.MaxTitle {
			t.Errorf("%s: title of %d characters", l.Key, n)
		}
		if l.Price < catalogdomain.MinPrice || l.Price > catalogdomain.MaxPrice {
			t.Errorf("%s: price %d", l.Key, l.Price)
		}
		if utf8.RuneCountInString(l.Description) > catalogdomain.MaxDescription {
			t.Errorf("%s: description of more than %d characters", l.Key, catalogdomain.MaxDescription)
		}
		if l.Condition != "new" && l.Condition != "like_new" && l.Condition != "used" {
			t.Errorf("%s: condition %q", l.Key, l.Condition)
		}
		if len(l.Photos) < 1 || len(l.Photos) > catalogdomain.MaxPhotos {
			t.Errorf("%s: %d photos", l.Key, len(l.Photos))
		}
		for _, f := range l.Photos {
			if _, err := data.ReadFile("data/photos/" + f); err != nil {
				t.Errorf("%s: %v", l.Key, err)
			}
		}
		if len(l.Specs) > catalogdomain.MaxSpecs {
			t.Errorf("%s: %d specs", l.Key, len(l.Specs))
		}
	}
	for seller, n := range onSale {
		if n > catalogdomain.MaxOnSale {
			t.Errorf("%s has %d listings on sale; the limit is %d", seller, n, catalogdomain.MaxOnSale)
		}
	}
	for _, key := range []string{"u-linh", "u-kiet", "u-nam"} {
		if _, ok := people[key]; !ok {
			t.Errorf("the deals need %s", key)
		}
	}
	for _, key := range []string{"p-iphone13", "p-iphone12", "p-iphone14", "p-sofa", "p-a7"} {
		if _, ok := listings[key]; !ok {
			t.Errorf("the deals need %s", key)
		}
	}
	if l := listings["p-iphone13"]; l.Seller != "u-kiet" || !l.AllowOffers || 4_800_000 >= l.Price || 4_800_000*2 < l.Price {
		t.Errorf("Linh's offer of 4,800,000 does not fit p-iphone13: %+v", l)
	}
	if l := listings["p-iphone12"]; l.Seller != "u-nam" || l.Price != 6_900_000 {
		t.Errorf("Linh accepts p-iphone12 at 6,900,000: %+v", l)
	}
	if !people["u-kiet"].Carrier || !people["u-nam"].Carrier {
		t.Error("Kiệt and Nam sell, so they link GHTK")
	}
}

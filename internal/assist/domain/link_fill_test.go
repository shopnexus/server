package domain_test

import (
	"errors"
	"testing"

	"github.com/shopnexus/server/internal/assist/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
)

func TestStripPhones(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, out string
		removed bool
	}{
		{"Liên hệ 0909 123 456 hoặc zalo 0909123456.", "Liên hệ hoặc zalo.", true},
		{"Gọi 0909.123.456, có ship.", "Gọi, có ship.", true},
		{"SĐT +84 909 123 456", "SĐT", true},
		{"Máy zin.\nLH 0976-421-234\nCó bớt", "Máy zin.\nLH\nCó bớt", true},
		{"Giá 4.500.000đ, pin 4700mAh, 1.200.000.000 đồng", "Giá 4.500.000đ, pin 4700mAh, 1.200.000.000 đồng", false},
		{"IMEI 012345678901234", "IMEI 012345678901234", false},
		{"Dùng 3 năm, màn 6.1 inch", "Dùng 3 năm, màn 6.1 inch", false},
	}
	for _, tc := range cases {
		out, removed := domain.StripPhones(tc.in)
		if out != tc.out || removed != tc.removed {
			t.Errorf("%q: %q %v, want %q %v", tc.in, out, removed, tc.out, tc.removed)
		}
	}
}

func iphone() domain.LinkAnswer {
	return domain.LinkAnswer{
		IsItem: true, Title: "  iPhone 11 64GB   Trắng, pin 81% ", Condition: "used",
		Specs:       []catalogdomain.Spec{{Label: "Dung lượng", Value: "64 GB"}, {Label: " ", Value: "x"}},
		Description: "Máy dùng 3 năm, zin all. Có thương lượng. Liên hệ 0909 123 456.",
		Negotiable:  true, ContactRemoved: false,
		Estimate: &domain.Range{Low: 4_000_000, Median: 4_500_000, High: 5_004_000},
	}
}

func TestCleanLink(t *testing.T) {
	t.Parallel()
	d, rng, removed, err := domain.CleanLink(iphone(), 4_500_000)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "iPhone 11 64GB Trắng, pin 81%" || d.Condition != catalogdomain.ConditionUsed ||
		len(d.Specs) != 1 || d.Description != "Máy dùng 3 năm, zin all. Có thương lượng. Liên hệ." || d.Price == nil ||
		*d.Price != 4_500_000 || !d.AllowOffers || d.Photos == nil || len(d.Photos) != 0 {
		t.Fatalf("%+v", d)
	}
	if !removed {
		t.Fatal("the regex took a phone out, so contact_removed")
	}
	if rng == nil || rng.High != 5_000_000 {
		t.Fatalf("range %+v", rng)
	}

	a := iphone()
	a.Condition, a.Estimate, a.ContactRemoved = "broken", nil, true
	a.Description = "Máy đẹp."
	d, rng, removed, err = domain.CleanLink(a, 5_000)
	if err != nil || d.CategoryCode != "" || d.Condition != catalogdomain.ConditionUsed || d.Price != nil || rng != nil || !removed {
		t.Fatalf("%+v %+v %v %v", d, rng, removed, err)
	}

	a = iphone()
	a.IsItem = false
	if _, _, _, err = domain.CleanLink(a, 4_500_000); !errors.Is(err, domain.ErrNoItem) {
		t.Fatalf("not an item: %v", err)
	}
	a = iphone()
	a.Title = "   "
	if _, _, _, err = domain.CleanLink(a, 4_500_000); !errors.Is(err, domain.ErrNoItem) {
		t.Fatalf("no title: %v", err)
	}
}

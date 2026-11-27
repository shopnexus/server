package domain_test

import (
	"errors"
	"testing"

	"github.com/shopnexus/server/internal/assist/domain"
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

func TestAdDraft(t *testing.T) {
	t.Parallel()
	d, removed, err := domain.AdDraft(item("iPhone 11 64GB Trắng", "Máy zin. Liên hệ 0912 345 678."), 4_500_000)
	if err != nil || !removed || d.Description != "Máy zin. Liên hệ." || d.Price == nil || *d.Price != 4_500_000 || len(d.Photos) != 0 {
		t.Fatalf("%+v %v %v", d, removed, err)
	}
	if d, removed, err = domain.AdDraft(item("iPhone 11", "Máy zin."), 5_000); err != nil || removed || d.Price != nil {
		t.Fatalf("a price under 10,000: %+v %v %v", d, removed, err)
	}
	if _, _, err = domain.AdDraft(domain.Reply{}, 4_500_000); !errors.Is(err, domain.ErrNoItem) {
		t.Fatalf("no item: %v", err)
	}
}

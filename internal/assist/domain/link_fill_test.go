package domain_test

import (
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

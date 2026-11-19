package domain_test

import (
	"testing"

	"github.com/shopnexus/server/internal/catalog/domain"
)

func TestSlug(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, title, code, want string }{
		{"accents and đ", "Xe đạp Giant Escape 3", "k7m2qx9a", "xe-dap-giant-escape-3-k7m2qx9a"},
		{"punctuation is one hyphen", "iPhone 13 – 256GB (còn BH)!", "k7m2qx9a", "iphone-13-256gb-con-bh-k7m2qx9a"},
		{"no words", "🔥🔥", "k7m2qx9a", "k7m2qx9a"},
		{
			"cut at a word", "Bàn làm việc gỗ sồi tự nhiên chân sắt sơn tĩnh điện kích thước một mét hai", "k7m2qx9a",
			"ban-lam-viec-go-soi-tu-nhien-chan-sat-son-tinh-dien-kich-k7m2qx9a",
		},
		{"no code", "Xe đạp", "", ""},
	} {
		if got := domain.Slug(c.title, c.code); got != c.want {
			t.Errorf("%s: Slug(%q) = %q, want %q", c.name, c.title, got, c.want)
		}
	}
}

func TestCodeOf(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ slug, want string }{
		{"xe-dap-giant-escape-3-k7m2qx9a", "k7m2qx9a"},
		{"k7m2qx9a", "k7m2qx9a"},
		{"xe-dap-K7M2QX9A", "k7m2qx9a"},
		{"xe-dap", ""},
		{"xe-dap-k7m2qx9i", ""}, // i is not in the alphabet
		{"0193a1b2-4248-73ad-8361-58d52805d45b", ""},
		{"", ""},
	} {
		if got := domain.CodeOf(c.slug); got != c.want {
			t.Errorf("CodeOf(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

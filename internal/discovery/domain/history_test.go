package domain_test

import (
	"testing"

	"github.com/shopnexus/server/internal/discovery/domain"
)

func TestHistoryKey(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Xe Đạp":            "xe dap",
		"  xe   đạp  ":      "xe dap",
		"xe dap":            "xe dap",
		"ĐIỆN THOẠI cũ":     "dien thoai cu",
		"iPhone 13 Pro Max": "iphone 13 pro max",
	} {
		if got := domain.HistoryKey(in); got != want {
			t.Errorf("HistoryKey(%q) = %q, want %q", in, got, want)
		}
	}
}

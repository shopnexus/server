package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/locale/localetest"
)

// Every key the code writes is in text/<tag>.json.
func TestTextKeys(t *testing.T) {
	t.Parallel()
	localetest.Keys(t, domain.Words, ".", map[string]int{"say": 0})
}

// Short amounts read as the shared money_short cases.
func TestMoneyShort(t *testing.T) {
	t.Parallel()
	for _, c := range localetest.FormatCases(t, "money_short") {
		var n int64
		if err := json.Unmarshal(c.Input, &n); err != nil {
			t.Fatal(err)
		}
		for tag, want := range c.Want {
			if got := domain.Money(tag, n); got != want {
				t.Errorf("%d %s: %q, want %q", n, tag, got, want)
			}
		}
	}
}

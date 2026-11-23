package locale_test

import (
	"testing"
	"time"

	"github.com/shopnexus/server/internal/platform/locale"
)

func TestMatch(t *testing.T) {
	t.Parallel()
	for header, want := range map[string]locale.Tag{
		"": "", "en-US,en;q=0.9": locale.English, "vi-VN,vi;q=0.9,en;q=0.8": locale.Vietnamese,
		"fr-FR,en;q=0.5": locale.English, "ja-JP": "", "not a header;;": "",
	} {
		if got := locale.Match(header); got != want {
			t.Errorf("%q: %q, want %q", header, got, want)
		}
	}
	if locale.Of("en") != locale.English || locale.Of("xx") != locale.Vietnamese || locale.Of("") != locale.Vietnamese {
		t.Error("Of")
	}
}

func TestMoneyAndDate(t *testing.T) {
	t.Parallel()
	if got := locale.Money(locale.Vietnamese, 3_800_000); got != "3.800.000 ₫" {
		t.Errorf("vi %q", got)
	}
	if got := locale.Money(locale.English, 3_800_000); got != "3,800,000 ₫" {
		t.Errorf("en %q", got)
	}
	at := time.Date(2026, 11, 29, 20, 0, 0, 0, time.UTC) // 30/11 in Vietnam
	if vi, en := locale.Date(locale.Vietnamese, at), locale.Date(locale.English, at); vi != "30/11" || en != "Nov 30" {
		t.Errorf("dates %q %q", vi, en)
	}
}

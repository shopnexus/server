package locale_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/locale/localetest"
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

// Negotiate reads the header, falls back to the default and marks the
// response as varying by it.
func TestNegotiate(t *testing.T) {
	t.Parallel()
	for header, want := range map[string]locale.Tag{"en-GB": locale.English, "": locale.Vietnamese, "ja": locale.Vietnamese} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		r.Header.Set("Accept-Language", header)
		w := httptest.NewRecorder()
		if got := locale.Negotiate(w, r); got != want {
			t.Errorf("%q: %q, want %q", header, got, want)
		}
		if got := w.Header().Get("Vary"); got != "Accept-Language" {
			t.Errorf("%q: Vary %q", header, got)
		}
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
	if vi, en := locale.Decimal(locale.Vietnamese, 1.25), locale.Decimal(locale.English, 1.25); vi != "1,25" || en != "1.25" {
		t.Errorf("decimals %q %q", vi, en)
	}
	at := time.Date(2026, 11, 29, 20, 0, 0, 0, time.UTC) // 30/11 in Vietnam
	if vi, en := locale.Date(locale.Vietnamese, at), locale.Date(locale.English, at); vi != "30/11" || en != "Nov 30" {
		t.Errorf("dates %q %q", vi, en)
	}
}

// The server writes the shared cases as the app and the website do.
func TestFormatCases(t *testing.T) {
	t.Parallel()
	write := map[string]func(t *testing.T, tag locale.Tag, input json.RawMessage) string{
		"money": func(t *testing.T, tag locale.Tag, input json.RawMessage) string {
			t.Helper()
			var n int64
			mustDecode(t, input, &n)
			return locale.Money(tag, n)
		},
		"decimal": func(t *testing.T, tag locale.Tag, input json.RawMessage) string {
			t.Helper()
			var f float64
			mustDecode(t, input, &f)
			return locale.Decimal(tag, f)
		},
		"day_month": func(t *testing.T, tag locale.Tag, input json.RawMessage) string {
			t.Helper()
			var at time.Time
			mustDecode(t, input, &at)
			return locale.Date(tag, at)
		},
	}
	for kind, f := range write {
		for _, c := range localetest.FormatCases(t, kind) {
			for tag, want := range c.Want {
				if got := f(t, tag, c.Input); got != want {
					t.Errorf("%s %s %s: %q, want %q", kind, c.Input, tag, got, want)
				}
			}
		}
	}
}

func mustDecode(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

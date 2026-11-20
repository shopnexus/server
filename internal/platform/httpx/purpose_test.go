package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopnexus/server/internal/platform/httpx"
)

func TestPrefetch(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{
		"":                                false,
		"prefetch":                        true,
		"prefetch;prerender":              true,
		" prefetch ; anonymous-client-ip": true,
		"prefetch, other":                 true,
		"prerender":                       false,
		"other, prefetch":                 true,
		"prefetcher":                      false,
		"Prefetch":                        false,
	} {
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", http.NoBody)
		if value != "" {
			r.Header.Set("Sec-Purpose", value)
		}
		if got := httpx.Prefetch(r); got != want {
			t.Errorf("Sec-Purpose %q: %v, want %v", value, got, want)
		}
	}
}

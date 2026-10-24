package e2e

import (
	"net/http"
	"testing"
)

func TestStaffCORSPreflight(t *testing.T) {
	t.Parallel()
	// A preflight is not an operation in the spec, so this calls the server
	// without call.
	preflight := func(path, origin string) (int, http.Header) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, server+path, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "x-requested-with")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode, res.Header
	}
	if status, h := preflight("/v1/staff/session/refresh", "http://console.e2e"); status != http.StatusNoContent ||
		h.Get("Access-Control-Allow-Origin") != "http://console.e2e" || h.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("the console: %d %v", status, h)
	}
	if _, h := preflight("/v1/staff/session/refresh", "http://evil.e2e"); h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("another origin: %v", h)
	}
	if _, h := preflight("/v1/accounts/me", "http://console.e2e"); h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("the customer API: %v", h)
	}
}

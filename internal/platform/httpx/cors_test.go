package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := CORS("http://console.test", "/v1/staff/")(next)
	serve := func(method, path, origin string, preflight bool) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if preflight {
			r.Header.Set("Access-Control-Request-Method", "POST")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := serve(http.MethodOptions, "/v1/staff/session/refresh", "http://console.test", true)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "http://console.test" ||
		w.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		w.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type, X-Requested-With" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	if w := serve(http.MethodGet, "/v1/staff/me", "http://console.test", false); w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") != "http://console.test" {
		t.Fatalf("a request from the console: %d %v", w.Code, w.Header())
	}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"another origin":   serve(http.MethodOptions, "/v1/staff/me", "http://evil.test", true),
		"the customer API": serve(http.MethodOptions, "/v1/accounts/me", "http://console.test", true),
		"no origin":        serve(http.MethodGet, "/v1/staff/me", "", false),
	} {
		if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code != http.StatusTeapot {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
	}
}

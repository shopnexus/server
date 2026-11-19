package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := CORS("http://console.test", "/v1/console/")(next)
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

	w := serve(http.MethodOptions, "/v1/console/login", "http://console.test", true)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "http://console.test" ||
		w.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		w.Header().Get("Access-Control-Allow-Headers") != "Content-Type, X-Requested-With, Idempotency-Key" ||
		w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, PUT, PATCH, DELETE" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	if w := serve(http.MethodGet, "/v1/console/me", "http://console.test", false); w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") != "http://console.test" {
		t.Fatalf("a request from the console: %d %v", w.Code, w.Header())
	}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"another origin":   serve(http.MethodOptions, "/v1/console/me", "http://evil.test", true),
		"the customer API": serve(http.MethodOptions, "/v1/accounts/me", "http://console.test", true),
		"no origin":        serve(http.MethodGet, "/v1/console/me", "", false),
	} {
		if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code != http.StatusTeapot {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
	}
}

func TestDocsCORS(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := DocsCORS([]string{"https://docs.test", "http://localhost:4321"})(next)
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

	w := serve(http.MethodOptions, "/v1/deals", "https://docs.test", true)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://docs.test" ||
		w.Header().Get("Access-Control-Allow-Credentials") != "" ||
		w.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type, Idempotency-Key" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"a call":           serve(http.MethodGet, "/v1/me/deals", "http://localhost:4321", false),
		"the health check": serve(http.MethodGet, "/healthz", "https://docs.test", false),
	} {
		if w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") == "" ||
			w.Header().Get("Access-Control-Expose-Headers") != "Trace-Id, Location" {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
	}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"another origin":   serve(http.MethodOptions, "/v1/deals", "https://evil.test", true),
		"the console":      serve(http.MethodOptions, "/v1/console/me", "https://docs.test", true),
		"a dashboard path": serve(http.MethodGet, "/_dashboard/callback", "https://docs.test", false),
		"no origin":        serve(http.MethodGet, "/v1/me/deals", "", false),
	} {
		if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code != http.StatusTeapot {
			t.Errorf("%s: %d %v", name, w.Code, w.Header())
		}
	}
}

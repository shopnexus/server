package httpx

import (
	"net/http"
	"slices"
	"strings"
)

// CORS answers cross-origin requests from origin to paths under prefix, with
// credentials so the console session cookie travels. Other origins and other
// paths get no CORS headers, so the browser refuses them; the customer API
// is called from the app, not a browser.
func CORS(origin, prefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, prefix) || r.Header.Get("Origin") != origin {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With, Idempotency-Key")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// DocsCORS answers cross-origin requests from the docs site's API
// playground: the customer API under /v1/ and the health checks, for one
// of origins. It sends no credentials, since the API takes a bearer token
// and the console paths, whose cookie must not travel, are left out.
func DocsCORS(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			p := r.URL.Path
			public := (strings.HasPrefix(p, "/v1/") && !strings.HasPrefix(p, "/v1/console/")) ||
				p == "/healthz" || p == "/readyz"
			if !public || origin == "" || !slices.Contains(origins, origin) {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", "Trace-Id, Location")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

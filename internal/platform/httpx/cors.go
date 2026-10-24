package httpx

import (
	"net/http"
	"strings"
)

// CORS answers cross-origin requests from origin to paths under prefix, with
// credentials so the staff session cookie travels. Other origins and other
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
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT")
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

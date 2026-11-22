package httpx

import (
	"net/http"
	"testing"
)

func TestRouterRoute(t *testing.T) {
	t.Parallel()
	r := NewRouter()
	r.HandleFunc("GET /v1/listings/{id}", func(http.ResponseWriter, *http.Request) {})
	for _, c := range []struct{ method, path, want string }{
		{"GET", "/v1/listings/42", "/v1/listings/{id}"},
		{"POST", "/v1/listings/42", ""},
		{"GET", "/v1/nope", ""},
	} {
		if got := r.Route(c.method, c.path); got != c.want {
			t.Errorf("Route(%s %s) = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

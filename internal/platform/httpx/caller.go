package httpx

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/authn"
)

// Caller returns the principal of a route behind the auth middleware, or
// writes 401 and reports false.
func Caller(w http.ResponseWriter, r *http.Request, log *slog.Logger) (authn.Principal, bool) {
	p, ok := authn.From(r.Context())
	if !ok {
		WriteError(w, r, log, authn.ErrUnauthenticated)
	}
	return p, ok
}

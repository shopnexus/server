package auth

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shopnexus/server2/internal/auth/session"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// RequireAuth checks the bearer token and puts the principal in the request
// context. It reads no database: a signed-out session's access token works
// until it expires, and write use cases check the account themselves.
func RequireAuth(v session.Verifier, now func() time.Time, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				httpx.WriteError(w, r, log, authn.ErrUnauthenticated)
				return
			}
			p, err := v.Verify(token, now())
			if err != nil {
				httpx.WriteError(w, r, log, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(authn.With(r.Context(), p)))
		})
	}
}

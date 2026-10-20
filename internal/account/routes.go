package account

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

// Routes registers the account endpoints behind protect, which requires a
// signed-in caller.
func Routes(r *httpx.Router, accounts Store, now func() time.Time, log *slog.Logger, protect func(http.Handler) http.Handler) {
	r.Handle("GET /v1/accounts/me", protect(GetHandler(accounts, now, log)))
	r.Handle("PATCH /v1/accounts/me", protect(RenameHandler(accounts, now, log)))
}

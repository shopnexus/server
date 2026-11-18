package stream

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// errUnloggedKind answers a kind the log does not hold, should a request
// reach the handler without the spec's check.
var errUnloggedKind = apperr.New(apperr.KindBadRequest, "invalid_request", "kind is not one the update log holds")

// DevUpdateHandler serves POST /v1/dev/updates, which app mounts outside
// APP_ENV=prod. It appends the hint in the body to the caller's log, so a
// test can watch an update reach the socket without making the change
// behind it. The spec has already checked that the IDs are the ones the
// kind carries; they need not name anything, since the log has no foreign
// key.
func DevUpdateHandler(updates PGLog, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		var h Hint
		if err := httpx.Bind(r, &h); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		if !slices.Contains(loggedKinds, h.Kind) {
			httpx.WriteError(w, r, log, errUnloggedKind)
			return
		}
		u, err := updates.Append(r.Context(), p.AccountID, h)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusCreated, u)
	})
}

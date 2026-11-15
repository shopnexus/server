package stream

import (
	"fmt"
	"log/slog"
	"net/http"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

// DevUpdateHandler serves POST /v1/dev/updates, which app mounts outside
// APP_ENV=prod. It logs the hint in the body for the caller as Notify does,
// so a test can watch an update reach the socket without making the change
// behind it. The spec has already checked that the IDs are the ones the
// kind carries; they need not name anything, since the log has no foreign
// key.
func DevUpdateHandler(db *pgxpool.Pool, log *slog.Logger) http.Handler {
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
		var u Update
		err := pgx.BeginFunc(r.Context(), db, func(tx pgx.Tx) error {
			targets, err := appendHint(r.Context(), tx, []uuid.UUID{p.AccountID}, h)
			if err != nil {
				return err
			}
			u = Update{Hint: h, Seq: targets[0].Seq}
			return nil
		})
		if err != nil {
			httpx.WriteError(w, r, log, fmt.Errorf("log the update: %w", err))
			return
		}
		httpx.Respond(w, http.StatusCreated, u)
	})
}

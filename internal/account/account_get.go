package account

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/postgres"
)

// AccountGet reads the caller's account. A read needs no transaction, so it
// runs on the pool.
type AccountGet struct {
	Store Store
	DB    postgres.Querier
	Now   func() time.Time
}

func (uc AccountGet) Execute(ctx context.Context, id uuid.UUID) (AccountView, error) {
	a, err := uc.Store.Get(ctx, uc.DB, id)
	if err != nil {
		return AccountView{}, err
	}
	return viewOf(a, uc.Now()), nil
}

// GET /v1/accounts/me
func (uc AccountGet) Handler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authn.From(r.Context())
		if !ok {
			httpx.WriteError(w, r, log, ErrNoPrincipal)
			return
		}
		out, err := uc.Execute(r.Context(), p.AccountID)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, r, http.StatusOK, out)
	})
}

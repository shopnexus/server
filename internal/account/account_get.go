package account

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// View is the Account schema in api/schemas/account.yaml.
type View struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

func viewOf(a Account, now time.Time) View {
	return View{ID: a.ID(), DisplayName: a.DisplayName(), Status: a.Status(now), CreatedAt: a.CreatedAt()}
}

// Get reads an account.
func Get(ctx context.Context, accounts Store, id uuid.UUID, now time.Time) (View, error) {
	a, err := accounts.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	return viewOf(a, now), nil
}

// GET /v1/accounts/me
func GetHandler(accounts Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authn.From(r.Context())
		if !ok {
			httpx.WriteError(w, r, log, authn.ErrUnauthenticated)
			return
		}
		v, err := Get(r.Context(), accounts, p.AccountID, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, r, http.StatusOK, v)
	})
}

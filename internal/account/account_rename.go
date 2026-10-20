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

// Rename sets the display name. It is a write, so a suspended account gets
// ErrSuspended even while its access token is still valid.
func Rename(ctx context.Context, accounts Store, id uuid.UUID, name string, now time.Time) (View, error) {
	a, err := accounts.Get(ctx, id)
	if err != nil {
		return View{}, err
	}
	if err := a.CanWrite(now); err != nil {
		return View{}, err
	}
	if err := a.SetDisplayName(name, now); err != nil {
		return View{}, err
	}
	if err := accounts.Save(ctx, &a, uuid.NewV7()); err != nil {
		return View{}, err
	}
	return viewOf(a, now), nil
}

type RenameRequest struct {
	DisplayName string `json:"display_name"`
}

// PATCH /v1/accounts/me
func RenameHandler(accounts Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authn.From(r.Context())
		if !ok {
			httpx.WriteError(w, r, log, authn.ErrUnauthenticated)
			return
		}
		in, err := httpx.Decode[RenameRequest](r)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		v, err := Rename(r.Context(), accounts, p.AccountID, in.DisplayName, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, r, http.StatusOK, v)
	})
}

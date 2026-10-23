package account

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// Rename sets the display name. It is a write, so a suspended account gets
// ErrSuspended even while its access token is still valid.
func Rename(ctx context.Context, accounts Store, id uuid.UUID, name string, now time.Time) (View, error) {
	a, err := accounts.Get(ctx, id)
	if err != nil {
		return View{}, fmt.Errorf("get account: %w", err)
	}
	if err := a.CanWrite(now); err != nil {
		return View{}, err
	}
	if err := a.SetDisplayName(name, now); err != nil {
		return View{}, err
	}
	if err := accounts.Save(ctx, &a, uuid.NewV7()); err != nil {
		if errors.Is(err, ErrConflict) {
			return View{}, apperr.ErrTryAgain
		}
		return View{}, fmt.Errorf("save account: %w", err)
	}
	return viewOf(a, now), nil
}

type RenameRequest struct {
	DisplayName string `json:"display_name"`
}

// RenameHandler serves PATCH /v1/accounts/me.
func RenameHandler(accounts Store, now func() time.Time, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := httpx.Caller(w, r, log)
		if !ok {
			return
		}
		var in RenameRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		v, err := Rename(r.Context(), accounts, p.AccountID, in.DisplayName, now())
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, v)
	})
}

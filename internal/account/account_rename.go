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

type AccountRenameInput struct {
	DisplayName string `json:"display_name"`
}

// AccountRename sets the display name. It is a write, so a suspended account
// gets ErrSuspended even while its access token is still valid.
type AccountRename struct {
	Store Store
	Tx    postgres.TxRunner
	Now   func() time.Time
}

func (uc AccountRename) Execute(ctx context.Context, id uuid.UUID, in AccountRenameInput) (AccountView, error) {
	var out AccountView
	err := uc.Tx.InTx(ctx, func(q postgres.Querier) error {
		a, err := uc.Store.GetForUpdate(ctx, q, id)
		if err != nil {
			return err
		}
		now := uc.Now()
		if err := a.CanWrite(now); err != nil {
			return err
		}
		if err := a.SetDisplayName(in.DisplayName, now); err != nil {
			return err
		}
		if err := uc.Store.Update(ctx, q, a); err != nil {
			return err
		}
		out = viewOf(a, now)
		return nil
	})
	return out, err
}

// PATCH /v1/accounts/me
func (uc AccountRename) Handler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := authn.From(r.Context())
		if !ok {
			httpx.WriteError(w, r, log, ErrNoPrincipal)
			return
		}
		in, err := httpx.Decode[AccountRenameInput](r)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		out, err := uc.Execute(r.Context(), p.AccountID, in)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, r, http.StatusOK, out)
	})
}

package staff

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/auth"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

// historyLength is how many staff actions the account screen shows.
const historyLength = 20

// AccountDetail is the StaffAccountDetail schema.
type AccountDetail struct {
	Account    account.Record  `json:"account"`
	Identities []auth.Identity `json:"identities"`
	Actions    []audit.Action  `json:"actions"`
}

type AccountRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetAccount reads an account with its identities and the latest staff
// actions on it.
func GetAccount(ctx context.Context, accounts AccountPort, identities IdentityPort, actions audit.Store, id uuid.UUID) (AccountDetail, error) {
	r, err := accounts.Record(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("get account: %w", err)
	}
	ids, err := identities.Identities(ctx, id)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list identities: %w", err)
	}
	history := page.Request{Cursor: uuid.UUID{}, Limit: historyLength}
	acts, err := actions.List(ctx, audit.Filter{StaffID: uuid.UUID{}, Action: "", TargetID: id, Since: time.Time{}, Until: time.Time{}}, history)
	if err != nil {
		return AccountDetail{}, fmt.Errorf("list staff actions: %w", err)
	}
	acts, _ = page.Cut(acts, history)
	if acts == nil {
		acts = []audit.Action{}
	}
	return AccountDetail{Account: r, Identities: ids, Actions: acts}, nil
}

// GetAccountHandler serves GET /v1/staff/accounts/{id}.
func GetAccountHandler(accounts AccountPort, identities IdentityPort, actions audit.Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in AccountRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		d, err := GetAccount(r.Context(), accounts, identities, actions, in.ID)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, d)
	})
}

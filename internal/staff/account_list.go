package staff

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/account"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type ListAccountsRequest struct {
	page.Request

	Q string `json:"-" query:"q"`
}

// ListAccounts lists accounts newest first or, with q, the one account q
// names by ID, phone number or email. A q that names nothing gives an empty
// page.
func ListAccounts(ctx context.Context, accounts AccountPort, identities IdentityPort, q string, p page.Request) (page.Page[account.Record], error) {
	if q == "" {
		rows, err := accounts.Records(ctx, p)
		if err != nil {
			return page.Page[account.Record]{}, fmt.Errorf("list accounts: %w", err)
		}
		return page.Of(rows, p, func(r account.Record) uuid.UUID { return r.ID }), nil
	}
	out := page.Page[account.Record]{Items: []account.Record{}}
	id, parseErr := uuid.Parse(q)
	if parseErr != nil {
		found, ok, err := identities.FindAccount(ctx, q)
		if err != nil {
			return page.Page[account.Record]{}, fmt.Errorf("find account: %w", err)
		}
		if !ok {
			return out, nil
		}
		id = found
	}
	r, err := accounts.Record(ctx, id)
	if errors.Is(err, account.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return page.Page[account.Record]{}, fmt.Errorf("get account: %w", err)
	}
	out.Items = append(out.Items, r)
	return out, nil
}

// ListAccountsHandler serves GET /v1/staff/accounts.
func ListAccountsHandler(accounts AccountPort, identities IdentityPort, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in ListAccountsRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		out, err := ListAccounts(r.Context(), accounts, identities, in.Q, in.Request)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, out)
	})
}

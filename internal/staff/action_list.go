package staff

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type ListActionsRequest struct {
	page.Request
	audit.Filter
}

// ListActions lists the audit log newest first, narrowed by f.
func ListActions(ctx context.Context, actions audit.Store, f audit.Filter, p page.Request) (page.Page[audit.Action], error) {
	rows, err := actions.List(ctx, f, p)
	if err != nil {
		return page.Page[audit.Action]{}, fmt.Errorf("list staff actions: %w", err)
	}
	rows, more := page.Cut(rows, p)
	out := page.Page[audit.Action]{Items: make([]audit.Action, 0, len(rows))}
	out.Items = append(out.Items, rows...)
	if more {
		out.NextCursor = new(rows[len(rows)-1].ID)
	}
	return out, nil
}

// ListActionsHandler serves GET /v1/staff/actions.
func ListActionsHandler(actions audit.Store, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in ListActionsRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		out, err := ListActions(r.Context(), actions, in.Filter, in.Request)
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, out)
	})
}

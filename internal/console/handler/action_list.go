package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type listActionsRequest struct {
	page.Request
	audit.Filter
}

// ListActions serves GET /v1/console/actions.
func (h *Handler) ListActions(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in listActionsRequest) (page.Page[audit.Action], error) {
		return h.svc.ListActions(r.Context(), in.Filter, in.Request)
	})
}

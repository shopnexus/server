package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type listActionsRequest struct {
	page.Request
	audit.Filter
}

// ListActions serves GET /v1/staff/actions.
func (h *Handler) ListActions(w http.ResponseWriter, r *http.Request) {
	var in listActionsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.ListActions(r.Context(), in.Filter, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

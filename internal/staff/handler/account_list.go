package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type listAccountsRequest struct {
	page.Request

	Q string `json:"-" query:"q"`
}

// ListAccounts serves GET /v1/staff/accounts.
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	var in listAccountsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.ListAccounts(r.Context(), in.Q, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

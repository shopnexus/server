package handler

import (
	"net/http"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/console/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type listAccountsRequest struct {
	page.Request

	Q string `json:"-" query:"q"`
	// Status is empty or suspended; the spec refuses any other.
	Status accountdomain.Status `json:"-" query:"status"`
}

// ListAccounts serves GET /v1/console/accounts.
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	var in listAccountsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.ListAccounts(r.Context(), service.AccountQuery{Q: in.Q, Suspended: in.Status == accountdomain.StatusSuspended}, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

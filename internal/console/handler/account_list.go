package handler

import (
	"net/http"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type listAccountsRequest struct {
	page.Request

	Q string `json:"-" query:"q"`
	// Status is empty or suspended; the spec refuses any other.
	Status accountdomain.Status `json:"-" query:"status"`
}

// ListAccounts serves GET /v1/console/accounts.
func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in listAccountsRequest) (service.AccountList, error) {
		return h.svc.ListAccounts(r.Context(), service.AccountQuery{Q: in.Q, Suspended: in.Status == accountdomain.StatusSuspended}, in.Request)
	})
}

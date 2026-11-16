package handler

import (
	"net/http"

	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type ordersRequest struct {
	page.Request

	Role dealdomain.ListRole `json:"-" query:"role"`
}

// Orders serves GET /v1/screens/orders.
func (h *Handler) Orders(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in ordersRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Orders(r.Context(), p.AccountID, in.Role.Party(), in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

package handler

import (
	"net/http"

	dealdomain "github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/screen/service"
)

type ordersRequest struct {
	page.Request

	Role dealdomain.ListRole `json:"-" query:"role"`
}

// Orders serves GET /v1/screens/orders.
func (h *Handler) Orders(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in ordersRequest) (service.OrdersScreen, error) {
		return h.svc.Orders(r.Context(), p.AccountID, in.Role.Party(), in.Request)
	})
}

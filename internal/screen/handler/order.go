package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
)

type orderRequest struct {
	OrderID uuid.UUID `json:"-" path:"order_id"`
}

func (h *Handler) Order(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in orderRequest) (service.OrderScreen, error) {
		return h.svc.Order(r.Context(), p.AccountID, in.OrderID)
	})
}

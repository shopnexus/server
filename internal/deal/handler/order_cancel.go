package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type cancelOrderRequest struct {
	OrderID uuid.UUID `json:"-"                 path:"order_id"`
	Key     uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// CancelOrder serves POST /v1/orders/{order_id}/cancel.
func (h *Handler) CancelOrder(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in cancelOrderRequest) (service.DealView, error) {
		return h.svc.CancelOrder(r.Context(), p.AccountID, in.OrderID, in.Key)
	})
}

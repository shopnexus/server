package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
)

type codCheckoutRequest struct {
	DealID    uuid.UUID  `json:"-" path:"deal_id"`
	AddressID *uuid.UUID `json:"-" query:"address_id"`
}

func (h *Handler) CodCheckout(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in codCheckoutRequest) (service.CodCheckoutScreen, error) {
		return h.svc.CodCheckout(r.Context(), p.AccountID, in.DealID, in.AddressID)
	})
}

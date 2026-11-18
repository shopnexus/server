package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
)

type codCheckoutRequest struct {
	DealID    uuid.UUID  `json:"-" path:"deal_id"`
	AddressID *uuid.UUID `json:"-" query:"address_id"`
}

func (h *Handler) CodCheckout(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in codCheckoutRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.CodCheckout(r.Context(), p.AccountID, in.DealID, in.AddressID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

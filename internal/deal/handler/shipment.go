package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
)

type lookupRequest struct {
	Carrier string `json:"-" query:"carrier"`
	Code    string `json:"-" query:"code"`
}

// LookupShipment serves GET /v1/shipments/lookup.
func (h *Handler) LookupShipment(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in lookupRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.LookupShipment(r.Context(), p.AccountID, in.Carrier, in.Code)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

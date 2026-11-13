package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type lookupRequest struct {
	Carrier string `json:"-" query:"carrier"`
	Code    string `json:"-" query:"code"`
}

type shipmentStatusRequest struct {
	Code   string `json:"-"      path:"code"`
	Status string `json:"status"`
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

// SetShipmentStatus serves POST /v1/dev/shipments/{code}/status, which
// Register adds only for the fake carrier.
func (h *Handler) SetShipmentStatus(w http.ResponseWriter, r *http.Request) {
	var in shipmentStatusRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.ShipmentUpdated(r.Context(), in.Code, in.Status); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

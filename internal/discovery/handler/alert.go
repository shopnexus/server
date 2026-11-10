package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type alertRequest struct {
	ListingID uuid.UUID `json:"-"     path:"listing_id"`
	Below     int64     `json:"below"`
}

// SetAlert serves PUT /v1/saved/{listing_id}/alert.
func (h *Handler) SetAlert(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in alertRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.SetAlert(r.Context(), p.AccountID, in.ListingID, in.Below); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ClearAlert serves DELETE /v1/saved/{listing_id}/alert.
func (h *Handler) ClearAlert(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.ClearAlert(r.Context(), p.AccountID, in.ListingID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PriceCutAlerts serves GET /v1/me/price-cut-alerts.
func (h *Handler) PriceCutAlerts(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.PriceCutAlerts(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type priceCutAlertsRequest struct {
	On bool `json:"on"`
}

// SetPriceCutAlerts serves PUT /v1/me/price-cut-alerts.
func (h *Handler) SetPriceCutAlerts(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in priceCutAlertsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.SetPriceCutAlerts(r.Context(), p.AccountID, in.On); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

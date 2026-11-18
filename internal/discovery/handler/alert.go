package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
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

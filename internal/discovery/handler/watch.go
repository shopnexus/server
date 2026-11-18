package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// watchRequest is a search to follow. The position reaches the service and
// no log.
type watchRequest struct {
	Query       string                    `json:"q"`
	Category    string                    `json:"category"`
	PriceMin    int64                     `json:"price_min"`
	PriceMax    int64                     `json:"price_max"`
	Conditions  []catalogdomain.Condition `json:"condition"`
	OffersOnly  bool                      `json:"offers_only"`
	NearCommune string                    `json:"near_commune"`
	NearLat     float64                   `json:"near_lat"`
	NearLng     float64                   `json:"near_lng"`
	RadiusKm    int                       `json:"radius_km"`
}

// Watch serves POST /v1/search/watches: 201 for a new watch, 200 for one
// that took new filters.
func (h *Handler) Watch(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in watchRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, created, err := h.svc.Watch(r.Context(), p.AccountID, service.WatchInput{
		Query: in.Query, Category: in.Category, PriceMin: in.PriceMin, PriceMax: in.PriceMax, Conditions: in.Conditions,
		OffersOnly: in.OffersOnly, Near: catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune}, RadiusKm: in.RadiusKm,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.Respond(w, status, out)
}

// Watches serves GET /v1/search/watches.
func (h *Handler) Watches(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.Watches(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type watchIDRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// Unwatch serves DELETE /v1/search/watches/{id}.
func (h *Handler) Unwatch(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in watchIDRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Unwatch(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Mute serves POST /v1/search/watches/{id}/mute.
func (h *Handler) Mute(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in watchIDRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.SetWatchAlert(r.Context(), p.AccountID, in.ID, false); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unmute serves POST /v1/search/watches/{id}/unmute.
func (h *Handler) Unmute(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in watchIDRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.SetWatchAlert(r.Context(), p.AccountID, in.ID, true); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

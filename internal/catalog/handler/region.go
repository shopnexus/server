package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// Provinces serves GET /v1/regions.
func (h *Handler) Provinces(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.Provinces(r.Context())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, list[domain.Province]{Items: ps})
}

type communesRequest struct {
	ProvinceCode string `json:"-" path:"province_code"`
}

// Communes serves GET /v1/regions/{province_code}/communes.
func (h *Handler) Communes(w http.ResponseWriter, r *http.Request) {
	var in communesRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	cs, err := h.svc.Communes(r.Context(), in.ProvinceCode)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, list[domain.Commune]{Items: cs})
}

type nearestRequest struct {
	Lat float64 `json:"-" query:"lat"`
	Lng float64 `json:"-" query:"lng"`
}

// Nearest serves GET /v1/regions/nearest. The spec makes lat and lng
// required and bounds them; the point is not logged.
func (h *Handler) Nearest(w http.ResponseWriter, r *http.Request) {
	var in nearestRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	loc, err := h.svc.Locate(r.Context(), domain.Point{Lat: in.Lat, Lng: in.Lng})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, loc)
}

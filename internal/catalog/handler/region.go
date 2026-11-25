package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// Provinces serves GET /v1/regions.
func (h *Handler) Provinces(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(httpx.None) (list[domain.Province], error) {
		ps, err := h.svc.Provinces(r.Context())
		return list[domain.Province]{Items: ps}, err
	})
}

type communesRequest struct {
	ProvinceCode string `json:"-" path:"province_code"`
}

// Communes serves GET /v1/regions/{province_code}/communes.
func (h *Handler) Communes(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in communesRequest) (list[domain.Commune], error) {
		cs, err := h.svc.Communes(r.Context(), in.ProvinceCode)
		return list[domain.Commune]{Items: cs}, err
	})
}

type nearestRequest struct {
	Lat float64 `json:"-" query:"lat"`
	Lng float64 `json:"-" query:"lng"`
}

// Nearest serves GET /v1/regions/nearest. The spec makes lat and lng
// required and bounds them; the point is not logged.
func (h *Handler) Nearest(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in nearestRequest) (service.Location, error) {
		return h.svc.Locate(r.Context(), domain.Point{Lat: in.Lat, Lng: in.Lng})
	})
}

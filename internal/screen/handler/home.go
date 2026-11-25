package handler

import (
	"net/http"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/screen/service"
)

// homeScreenRequest is where the viewer is, which reaches the service and
// no log, and how far the nearby feed reaches.
type homeScreenRequest struct {
	NearLat     float64 `json:"-" query:"near_lat"`
	NearLng     float64 `json:"-" query:"near_lng"`
	NearCommune string  `json:"-" query:"near_commune"`
	RadiusKm    int     `json:"-" query:"radius_km"`
}

// Home serves GET /v1/screens/home, signed in or not, with the categories
// named in the request's language.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in homeScreenRequest) (service.HomeScreen, error) {
		near := catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune}
		return h.svc.Home(r.Context(), httpx.Viewer(r), near, in.RadiusKm, locale.Negotiate(w, r))
	})
}

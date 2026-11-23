package handler

import (
	"net/http"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
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
	var in homeScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	near := catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune}
	out, err := h.svc.Home(r.Context(), httpx.Viewer(r), near, in.RadiusKm, locale.Negotiate(w, r))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

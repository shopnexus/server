package handler

import (
	"net/http"

	discoverysvc "github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// homeScreenRequest is where the viewer is. It reaches the service and no
// log.
type homeScreenRequest struct {
	NearLat     float64 `json:"-" query:"near_lat"`
	NearLng     float64 `json:"-" query:"near_lng"`
	NearCommune string  `json:"-" query:"near_commune"`
}

// Home serves GET /v1/screens/home, signed in or not.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	var in homeScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Home(r.Context(), httpx.Viewer(r), discoverysvc.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

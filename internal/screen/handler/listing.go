package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// listingScreenRequest carries where the reader is, which reaches the service and
// no log.
type listingScreenRequest struct {
	ID          uuid.UUID `json:"-" path:"id"`
	NearLat     float64   `json:"-" query:"near_lat"`
	NearLng     float64   `json:"-" query:"near_lng"`
	NearCommune string    `json:"-" query:"near_commune"`
}

// Listing serves GET /v1/screens/listing/{id}, signed in or not.
func (h *Handler) Listing(w http.ResponseWriter, r *http.Request) {
	var in listingScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Listing(r.Context(), httpx.Viewer(r), in.ID, catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// sellerScreenRequest carries where the reader is, which reaches the service and
// no log.
type sellerScreenRequest struct {
	ID          uuid.UUID `json:"-" path:"id"`
	NearLat     float64   `json:"-" query:"near_lat"`
	NearLng     float64   `json:"-" query:"near_lng"`
	NearCommune string    `json:"-" query:"near_commune"`
}

// Seller serves GET /v1/screens/seller/{id}, signed in or not.
func (h *Handler) Seller(w http.ResponseWriter, r *http.Request) {
	var in sellerScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Seller(r.Context(), httpx.Viewer(r), in.ID, catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

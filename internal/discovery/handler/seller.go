package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

// sellerListingsRequest carries where the reader is, which reaches the
// service and no log.
type sellerListingsRequest struct {
	page.Request

	ID          uuid.UUID `json:"-" path:"id"`
	NearLat     float64   `json:"-" query:"near_lat"`
	NearLng     float64   `json:"-" query:"near_lng"`
	NearCommune string    `json:"-" query:"near_commune"`
}

// SellerListings serves GET /v1/sellers/{id}/listings, signed in or not.
func (h *Handler) SellerListings(w http.ResponseWriter, r *http.Request) {
	var in sellerListingsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	near := catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune}
	out, err := h.svc.SellerListings(r.Context(), httpx.Viewer(r), in.ID, near, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
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

// SellerListings serves GET /v1/sellers/{id}/listings to anyone.
func (h *Handler) SellerListings(w http.ResponseWriter, r *http.Request) {
	var in sellerListingsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.SellerListings(r.Context(), in.ID, domain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune}, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

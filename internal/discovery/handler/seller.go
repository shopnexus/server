package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/discovery/service"
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
	httpx.Serve(w, r, h.log, http.StatusOK, func(in sellerListingsRequest) (page.Page[service.SearchItem], error) {
		near := catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune}
		return h.svc.SellerListings(r.Context(), httpx.Viewer(r), in.ID, near, in.Request)
	})
}

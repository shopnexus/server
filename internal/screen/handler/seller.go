package handler

import (
	"net/http"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
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
	httpx.Serve(w, r, h.log, http.StatusOK, func(in sellerScreenRequest) (service.SellerScreen, error) {
		return h.svc.Seller(r.Context(), httpx.Viewer(r), in.ID, catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune})
	})
}

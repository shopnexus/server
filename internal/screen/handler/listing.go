package handler

import (
	"net/http"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/screen/service"
)

// listingScreenRequest carries where the reader is, which reaches the service and
// no log.
type listingScreenRequest struct {
	// Ref is the listing's ID or the slug of its web address.
	Ref         string  `json:"-" path:"id"`
	NearLat     float64 `json:"-" query:"near_lat"`
	NearLng     float64 `json:"-" query:"near_lng"`
	NearCommune string  `json:"-" query:"near_commune"`
}

// Listing serves GET /v1/screens/listing/{id}, signed in or not. A
// prefetch (Sec-Purpose) reads the same view and counts no view.
func (h *Handler) Listing(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in listingScreenRequest) (service.ListingScreen, error) {
		return h.svc.Listing(r.Context(), httpx.Viewer(r), service.ListingRead{
			Ref:      in.Ref,
			Near:     catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune},
			Prefetch: httpx.Prefetch(r),
			Locale:   locale.Negotiate(w, r),
		})
	})
}

// soldListingRequest names the listing by its ID or slug.
type soldListingRequest struct {
	Ref string `json:"-" path:"id"`
}

// SoldListing serves GET /v1/screens/sold-listing/{id}, signed in or not.
func (h *Handler) SoldListing(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in soldListingRequest) (service.SoldListingScreen, error) {
		return h.svc.SoldListing(r.Context(), in.Ref)
	})
}

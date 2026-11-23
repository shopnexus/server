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
	var in listingScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Listing(r.Context(), httpx.Viewer(r), service.ListingRead{
		Ref:      in.Ref,
		Near:     catalogdomain.Near{Lat: in.NearLat, Lng: in.NearLng, Commune: in.NearCommune},
		Prefetch: httpx.Prefetch(r),
		Locale:   locale.Negotiate(w, r),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// soldListingRequest names the listing by its ID or slug.
type soldListingRequest struct {
	Ref string `json:"-" path:"id"`
}

// SoldListing serves GET /v1/screens/sold-listing/{id}, signed in or not.
func (h *Handler) SoldListing(w http.ResponseWriter, r *http.Request) {
	var in soldListingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.SoldListing(r.Context(), in.Ref)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// MarkViewed serves PUT /v1/viewed/{listing_id}, the open of a listing
// view the browser read as a prefetch.
func (h *Handler) MarkViewed(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in listingRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Viewed(r.Context(), p.AccountID, in.ListingID)
	})
}

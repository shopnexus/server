package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
)

// MarkViewed serves PUT /v1/viewed/{listing_id}, the open of a listing
// view the browser read as a prefetch.
func (h *Handler) MarkViewed(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Viewed(r.Context(), p.AccountID, in.ListingID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

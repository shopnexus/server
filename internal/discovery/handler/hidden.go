package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// Hide serves PUT /v1/hidden/{listing_id}.
func (h *Handler) Hide(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in listingRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Hide(r.Context(), p.AccountID, in.ListingID)
	})
}

// Unhide serves DELETE /v1/hidden/{listing_id}.
func (h *Handler) Unhide(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in listingRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Unhide(r.Context(), p.AccountID, in.ListingID)
	})
}

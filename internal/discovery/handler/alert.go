package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type alertRequest struct {
	ListingID uuid.UUID `json:"-"     path:"listing_id"`
	Below     int64     `json:"below"`
}

// SetAlert serves PUT /v1/saved/{listing_id}/alert.
func (h *Handler) SetAlert(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in alertRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.SetAlert(r.Context(), p.AccountID, in.ListingID, in.Below)
	})
}

// ClearAlert serves DELETE /v1/saved/{listing_id}/alert.
func (h *Handler) ClearAlert(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in listingRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.ClearAlert(r.Context(), p.AccountID, in.ListingID)
	})
}

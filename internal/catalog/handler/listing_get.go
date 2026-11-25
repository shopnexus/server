package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

// Get serves GET /v1/listings/{id}, signed in or not.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in listingRequest) (service.ListingView, error) {
		return h.svc.Get(r.Context(), httpx.Viewer(r), in.ID, locale.Negotiate(w, r))
	})
}

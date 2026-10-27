package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type sellerListingsRequest struct {
	page.Request

	ID uuid.UUID `json:"-" path:"id"`
}

// SellerListings serves GET /v1/sellers/{id}/listings to anyone.
func (h *Handler) SellerListings(w http.ResponseWriter, r *http.Request) {
	var in sellerListingsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.SellerListings(r.Context(), in.ID, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

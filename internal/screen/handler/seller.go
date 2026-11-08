package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type sellerScreenRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// Seller serves GET /v1/screens/seller/{id}, signed in or not.
func (h *Handler) Seller(w http.ResponseWriter, r *http.Request) {
	var in sellerScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Seller(r.Context(), httpx.Viewer(r), in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

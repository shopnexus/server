package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type listingScreenRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// Listing serves GET /v1/screens/listing/{id}, signed in or not.
func (h *Handler) Listing(w http.ResponseWriter, r *http.Request) {
	var in listingScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Listing(r.Context(), httpx.Viewer(r), in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

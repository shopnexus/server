package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
)

// Get serves GET /v1/listings/{id}, signed in or not.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Get(r.Context(), httpx.Viewer(r), in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

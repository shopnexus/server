package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type accountRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetAccount serves GET /v1/staff/accounts/{id}.
func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	var in accountRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	d, err := h.svc.GetAccount(r.Context(), in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, d)
}

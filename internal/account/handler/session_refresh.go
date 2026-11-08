package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh serves POST /v1/auth/refresh.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var in refreshRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Refresh(r.Context(), in.RefreshToken)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

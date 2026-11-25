package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh serves POST /v1/auth/refresh.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in refreshRequest) (domain.Tokens, error) {
		return h.svc.Refresh(r.Context(), in.RefreshToken)
	})
}

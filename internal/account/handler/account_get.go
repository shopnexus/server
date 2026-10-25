package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

// GetMe serves GET /v1/accounts/me.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	v, err := h.svc.Get(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

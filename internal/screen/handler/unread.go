package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
)

// Unread serves GET /v1/me/unread.
func (h *Handler) Unread(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.Unread(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

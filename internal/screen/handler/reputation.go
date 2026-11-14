package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

// Reputation serves GET /v1/screens/reputation.
func (h *Handler) Reputation(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.Reputation(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

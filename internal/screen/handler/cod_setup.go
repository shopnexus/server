package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
)

func (h *Handler) CodSetup(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	v, err := h.svc.CodSetup(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

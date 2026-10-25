package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type renameRequest struct {
	DisplayName string `json:"display_name"`
}

// Rename serves PATCH /v1/accounts/me.
func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in renameRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Rename(r.Context(), p.AccountID, in.DisplayName)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

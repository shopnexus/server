package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

// ProfileTasks serves GET /v1/screens/profile-tasks.
func (h *Handler) ProfileTasks(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.ProfileTasks(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
)

// ProfileTasks serves GET /v1/screens/profile-tasks.
func (h *Handler) ProfileTasks(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.ProfileTasksScreen, error) {
		return h.svc.ProfileTasks(r.Context(), p.AccountID)
	})
}

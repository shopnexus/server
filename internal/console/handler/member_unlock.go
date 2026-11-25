package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// UnlockMember serves POST /v1/console/members/{id}/unlock.
func (h *Handler) UnlockMember(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in memberRequest) (service.MemberView, error) {
		return h.svc.UnlockMember(r.Context(), in.ID, audit.ByMember(caller))
	})
}

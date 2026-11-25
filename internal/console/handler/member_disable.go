package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// memberRequest names a member by the path.
type memberRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// DisableMember serves POST /v1/console/members/{id}/disable.
func (h *Handler) DisableMember(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in memberRequest) (service.MemberView, error) {
		return h.svc.DisableMember(r.Context(), in.ID, audit.ByMember(caller))
	})
}

// EnableMember serves POST /v1/console/members/{id}/enable.
func (h *Handler) EnableMember(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in memberRequest) (service.MemberView, error) {
		return h.svc.EnableMember(r.Context(), in.ID, audit.ByMember(caller))
	})
}

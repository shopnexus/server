package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type renameMemberRequest struct {
	ID          uuid.UUID `json:"-"            path:"id"`
	DisplayName string    `json:"display_name"`
}

// RenameMember serves PATCH /v1/console/members/{id}.
func (h *Handler) RenameMember(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in renameMemberRequest) (service.MemberView, error) {
		return h.svc.RenameMember(r.Context(), in.ID, in.DisplayName, audit.ByMember(caller))
	})
}

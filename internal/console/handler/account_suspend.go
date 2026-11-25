package handler

import (
	"net/http"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type suspendRequest struct {
	ID     uuid.UUID  `json:"-"      path:"id"`
	Reason string     `json:"reason"`
	Until  *time.Time `json:"until"`
}

// SuspendAccount serves POST /v1/console/accounts/{id}/suspend.
func (h *Handler) SuspendAccount(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in suspendRequest) (accountsvc.ConsoleAccount, error) {
		return h.svc.SuspendAccount(r.Context(), accountsvc.SuspendInput{
			ID: in.ID, Reason: in.Reason, Until: in.Until, By: audit.ByMember(caller),
		})
	})
}

// UnsuspendAccount serves POST /v1/console/accounts/{id}/unsuspend.
func (h *Handler) UnsuspendAccount(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in accountRequest) (accountsvc.ConsoleAccount, error) {
		return h.svc.UnsuspendAccount(r.Context(), in.ID, audit.ByMember(caller))
	})
}

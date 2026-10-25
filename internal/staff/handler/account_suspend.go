package handler

import (
	"net/http"
	"time"
	"uuid"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type suspendRequest struct {
	ID     uuid.UUID  `json:"-"      path:"id"`
	Reason string     `json:"reason"`
	Until  *time.Time `json:"until"`
}

// SuspendAccount serves POST /v1/staff/accounts/{id}/suspend.
func (h *Handler) SuspendAccount(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.StaffCaller(w, r, h.log)
	if !ok {
		return
	}
	var in suspendRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	rec, err := h.svc.SuspendAccount(r.Context(), accountsvc.SuspendInput{
		ID: in.ID, Reason: in.Reason, Until: in.Until, By: audit.ByStaff(caller),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, rec)
}

// UnsuspendAccount serves POST /v1/staff/accounts/{id}/unsuspend.
func (h *Handler) UnsuspendAccount(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.StaffCaller(w, r, h.log)
	if !ok {
		return
	}
	var in accountRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	rec, err := h.svc.UnsuspendAccount(r.Context(), in.ID, audit.ByStaff(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, rec)
}

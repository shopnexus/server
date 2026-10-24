package staff

import (
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type SuspendRequest struct {
	ID     uuid.UUID  `json:"-"      path:"id"`
	Reason string     `json:"reason"`
	Until  *time.Time `json:"until"`
}

// SuspendAccountHandler serves POST /v1/staff/accounts/{id}/suspend.
func SuspendAccountHandler(accounts AccountPort, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		var in SuspendRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		rec, err := accounts.Suspend(r.Context(), in.ID, in.Reason, in.Until, audit.ByStaff(caller))
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, rec)
	})
}

// UnsuspendAccountHandler serves POST /v1/staff/accounts/{id}/unsuspend.
func UnsuspendAccountHandler(accounts AccountPort, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, ok := httpx.StaffCaller(w, r, log)
		if !ok {
			return
		}
		var in AccountRequest
		if err := httpx.Bind(r, &in); err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		rec, err := accounts.Reinstate(r.Context(), in.ID, audit.ByStaff(caller))
		if err != nil {
			httpx.WriteError(w, r, log, err)
			return
		}
		httpx.Respond(w, http.StatusOK, rec)
	})
}

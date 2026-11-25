package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type renameMemberRequest struct {
	ID          uuid.UUID `json:"-"            path:"id"`
	DisplayName string    `json:"display_name"`
}

// RenameMember serves PATCH /v1/console/members/{id}.
func (h *Handler) RenameMember(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in renameMemberRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.RenameMember(r.Context(), in.ID, in.DisplayName, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

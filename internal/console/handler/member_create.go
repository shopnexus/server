package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type createMemberRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// createdMember is the CreatedMember schema.
type createdMember struct {
	Member            service.MemberView `json:"member"`
	TemporaryPassword string             `json:"temporary_password"`
}

// CreateMember serves POST /v1/console/members. The API creates moderators
// only.
func (h *Handler) CreateMember(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in createMemberRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, temp, err := h.svc.CreateMember(r.Context(), service.CreateMemberInput{
		Email: in.Email, DisplayName: in.DisplayName, Role: domain.RoleModerator, By: audit.ByMember(caller),
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, createdMember{Member: v, TemporaryPassword: temp})
}

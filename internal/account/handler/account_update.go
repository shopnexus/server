package handler

import (
	"encoding/json"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type updateRequest struct {
	DisplayName    *string    `json:"display_name"`
	Bio            *string    `json:"bio"`
	AvatarUploadID nullableID `json:"avatar_upload_id"`
}

// nullableID is a body field that may be left out, null or a UUID.
type nullableID struct {
	ID   *uuid.UUID
	Null bool
}

func (n *nullableID) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.Null = true
		return nil
	}
	return json.Unmarshal(b, &n.ID)
}

// Update serves PATCH /v1/accounts/me.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in updateRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Update(r.Context(), p.AccountID, service.UpdateInput{
		DisplayName: in.DisplayName, Bio: in.Bio, Avatar: in.AvatarUploadID.ID, RemoveAvatar: in.AvatarUploadID.Null,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

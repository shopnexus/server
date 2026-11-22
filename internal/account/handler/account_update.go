package handler

import (
	"encoding/json"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type updateRequest struct {
	DisplayName    *string             `json:"display_name"`
	Bio            *string             `json:"bio"`
	AvatarUploadID nullable[uuid.UUID] `json:"avatar_upload_id"`
	AwayUntil      nullable[time.Time] `json:"away_until"`
}

// nullable is a body field that may be left out, null or a value.
type nullable[T any] struct {
	Value *T
	Null  bool
}

func (n *nullable[T]) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.Null = true
		return nil
	}
	return json.Unmarshal(b, &n.Value)
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
		DisplayName: in.DisplayName, Bio: in.Bio, Avatar: in.AvatarUploadID.Value, RemoveAvatar: in.AvatarUploadID.Null,
		AwayUntil: in.AwayUntil.Value, ClearAway: in.AwayUntil.Null,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

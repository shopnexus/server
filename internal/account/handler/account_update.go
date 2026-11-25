package handler

import (
	"encoding/json"
	"net/http"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
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
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in updateRequest) (service.AccountView, error) {
		return h.svc.Update(r.Context(), p.AccountID, service.UpdateInput{
			DisplayName: in.DisplayName, Bio: in.Bio, Avatar: in.AvatarUploadID.Value, RemoveAvatar: in.AvatarUploadID.Null,
			AwayUntil: in.AwayUntil.Value, ClearAway: in.AwayUntil.Null,
		})
	})
}

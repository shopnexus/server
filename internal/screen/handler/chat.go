package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/screen/service"
)

type chatScreenRequest struct {
	ConversationID uuid.UUID `json:"-" path:"conversation_id"`
}

// Chat serves GET /v1/screens/chat/{conversation_id}.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in chatScreenRequest) (service.ChatScreen, error) {
		return h.svc.Chat(r.Context(), p.AccountID, in.ConversationID, locale.Negotiate(w, r))
	})
}

package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

type chatScreenRequest struct {
	ConversationID uuid.UUID `json:"-" path:"conversation_id"`
}

// Chat serves GET /v1/screens/chat/{conversation_id}.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in chatScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Chat(r.Context(), p.AccountID, in.ConversationID, locale.Negotiate(w, r))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

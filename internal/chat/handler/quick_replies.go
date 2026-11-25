package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

// QuickReplies serves GET /v1/me/quick-replies, the defaults in the
// request's language.
func (h *Handler) QuickReplies(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.QuickReplies, error) {
		return h.svc.QuickReplies(r.Context(), p.AccountID, locale.Negotiate(w, r))
	})
}

type quickRepliesRequest struct {
	Items []string `json:"items"`
}

// PutQuickReplies serves PUT /v1/me/quick-replies.
func (h *Handler) PutQuickReplies(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in quickRepliesRequest) (service.QuickReplies, error) {
		return h.svc.PutQuickReplies(r.Context(), p.AccountID, in.Items)
	})
}

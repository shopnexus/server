package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

// QuickReplies serves GET /v1/me/quick-replies, the defaults in the
// request's language.
func (h *Handler) QuickReplies(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.QuickReplies(r.Context(), p.AccountID, locale.Negotiate(w, r))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type quickRepliesRequest struct {
	Items []string `json:"items"`
}

// PutQuickReplies serves PUT /v1/me/quick-replies.
func (h *Handler) PutQuickReplies(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in quickRepliesRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.PutQuickReplies(r.Context(), p.AccountID, in.Items)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

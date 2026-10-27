package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type inboxScreenRequest struct {
	page.Request

	Archived bool `json:"-" query:"archived"`
}

// Inbox serves GET /v1/screens/inbox.
func (h *Handler) Inbox(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in inboxScreenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Inbox(r.Context(), p.AccountID, in.Archived, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

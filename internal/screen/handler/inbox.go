package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/screen/service"
)

type inboxScreenRequest struct {
	page.Request

	Archived bool `json:"-" query:"archived"`
}

// Inbox serves GET /v1/screens/inbox.
func (h *Handler) Inbox(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in inboxScreenRequest) (service.InboxScreen, error) {
		return h.svc.Inbox(r.Context(), p.AccountID, in.Archived, in.Request, locale.Negotiate(w, r))
	})
}

type inboxSearchRequest struct {
	page.Request

	Q string `json:"-" query:"q"`
}

// InboxSearch serves GET /v1/screens/inbox/search.
func (h *Handler) InboxSearch(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in inboxSearchRequest) (service.InboxSearchScreen, error) {
		return h.svc.InboxSearch(r.Context(), p.AccountID, in.Q, in.Request, locale.Negotiate(w, r))
	})
}

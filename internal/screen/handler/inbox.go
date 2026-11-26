package handler

import (
	"net/http"

	chatdomain "github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/screen/service"
)

type inboxScreenRequest struct {
	page.Request

	Archived bool `json:"-" query:"archived"`
	Ignored  bool `json:"-" query:"ignored"`
}

// box is the box the request asks for: ignored lists the ignored
// conversations whatever archived says.
func (in inboxScreenRequest) box() chatdomain.Box {
	switch {
	case in.Ignored:
		return chatdomain.BoxIgnored
	case in.Archived:
		return chatdomain.BoxArchived
	}
	return chatdomain.BoxInbox
}

// Inbox serves GET /v1/screens/inbox.
func (h *Handler) Inbox(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in inboxScreenRequest) (service.InboxScreen, error) {
		return h.svc.Inbox(r.Context(), p.AccountID, in.box(), in.Request, locale.Negotiate(w, r))
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

package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

// ListSessions serves GET /v1/auth/sessions.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in page.Request) (page.Page[service.SessionItem], error) {
		return h.svc.ListSessions(r.Context(), p, in)
	})
}

package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

// ListMembers serves GET /v1/console/members.
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in page.Request) (page.Page[service.MemberView], error) {
		return h.svc.ListMembers(r.Context(), in)
	})
}

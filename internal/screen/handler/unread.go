package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
)

// Unread serves GET /v1/me/unread.
func (h *Handler) Unread(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.Unread, error) {
		return h.svc.Unread(r.Context(), p.AccountID)
	})
}

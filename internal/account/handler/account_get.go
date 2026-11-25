package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// GetMe serves GET /v1/accounts/me.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.MeView, error) {
		return h.svc.Me(r.Context(), p.AccountID)
	})
}

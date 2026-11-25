package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
)

func (h *Handler) CodSetup(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.CodSetupScreen, error) {
		return h.svc.CodSetup(r.Context(), p.AccountID)
	})
}

package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
)

// Reputation serves GET /v1/screens/reputation.
func (h *Handler) Reputation(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.ReputationScreen, error) {
		return h.svc.Reputation(r.Context(), p.AccountID)
	})
}

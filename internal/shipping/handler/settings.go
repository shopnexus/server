package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/shipping/domain"
)

func (h *Handler) PutSettings(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in domain.Settings) (domain.Settings, error) {
		return h.svc.PutSettings(r.Context(), p.AccountID, in)
	})
}

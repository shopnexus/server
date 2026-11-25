package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/screen/service"
)

// Offers serves GET /v1/screens/offers.
func (h *Handler) Offers(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in page.Request) (service.OffersScreen, error) {
		return h.svc.Offers(r.Context(), p.AccountID, in)
	})
}

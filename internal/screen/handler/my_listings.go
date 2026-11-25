package handler

import (
	"net/http"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/screen/service"
)

type myListingsRequest struct {
	page.Request

	Pane catalogsvc.Pane `json:"-" query:"pane"`
}

// MyListings serves GET /v1/screens/my-listings.
func (h *Handler) MyListings(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in myListingsRequest) (service.MyListingsScreen, error) {
		return h.svc.MyListings(r.Context(), p.AccountID, in.Pane, in.Request, locale.Negotiate(w, r))
	})
}

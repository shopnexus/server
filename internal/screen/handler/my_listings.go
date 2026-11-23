package handler

import (
	"net/http"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/page"
)

type myListingsRequest struct {
	page.Request

	Pane catalogsvc.Pane `json:"-" query:"pane"`
}

// MyListings serves GET /v1/screens/my-listings.
func (h *Handler) MyListings(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in myListingsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.MyListings(r.Context(), p.AccountID, in.Pane, in.Request, locale.Negotiate(w, r))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

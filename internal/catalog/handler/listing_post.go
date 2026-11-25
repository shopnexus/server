package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

type postRequest struct {
	listingBody

	Key uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// Post serves POST /v1/listings.
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusCreated, func(p authn.Principal, in postRequest) (service.ListingView, error) {
		return h.svc.Post(r.Context(), p.AccountID, in.Key, in.input(), locale.Negotiate(w, r))
	})
}

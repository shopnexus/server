package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

type updateRequest struct {
	listingBody

	ID uuid.UUID `json:"-" path:"id"`
}

// Update serves PUT /v1/listings/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in updateRequest) (service.ListingView, error) {
		return h.svc.Edit(r.Context(), p.AccountID, in.ID, in.input(), locale.Negotiate(w, r))
	})
}

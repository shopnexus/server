package handler

import (
	"context"
	"net/http"
	"uuid"

	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type listingRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// HideListing serves POST /v1/console/listings/{id}/hide.
func (h *Handler) HideListing(w http.ResponseWriter, r *http.Request) {
	h.moderateListing(w, r, h.catalog.Hold)
}

// UnhideListing serves POST /v1/console/listings/{id}/unhide.
func (h *Handler) UnhideListing(w http.ResponseWriter, r *http.Request) {
	h.moderateListing(w, r, h.catalog.Lift)
}

// moderateListing serves a member's change to a listing.
func (h *Handler) moderateListing(w http.ResponseWriter, r *http.Request,
	change func(context.Context, uuid.UUID, audit.Actor) (catalogsvc.ReportedListing, error),
) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in listingRequest) (catalogsvc.ReportedListing, error) {
		return change(r.Context(), in.ID, audit.ByMember(caller))
	})
}

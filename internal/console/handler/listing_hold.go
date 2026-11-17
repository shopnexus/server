package handler

import (
	"context"
	"net/http"
	"uuid"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/httpx"
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
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := change(r.Context(), in.ID, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

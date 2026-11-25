package handler

import (
	"context"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

type listingRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// Hide serves POST /v1/listings/{id}/hide.
func (h *Handler) Hide(w http.ResponseWriter, r *http.Request) { h.move(w, r, h.svc.Hide) }

// Unhide serves POST /v1/listings/{id}/unhide.
func (h *Handler) Unhide(w http.ResponseWriter, r *http.Request) { h.move(w, r, h.svc.Unhide) }

// MarkSold serves POST /v1/listings/{id}/mark-sold.
func (h *Handler) MarkSold(w http.ResponseWriter, r *http.Request) { h.move(w, r, h.svc.MarkSold) }

// UnmarkSold serves POST /v1/listings/{id}/unmark-sold.
func (h *Handler) UnmarkSold(w http.ResponseWriter, r *http.Request) { h.move(w, r, h.svc.UnmarkSold) }

// move serves a status change of the caller's listing.
func (h *Handler) move(w http.ResponseWriter, r *http.Request,
	change func(context.Context, uuid.UUID, uuid.UUID, locale.Tag) (service.ListingView, error),
) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in listingRequest) (service.ListingView, error) {
		return change(r.Context(), p.AccountID, in.ID, locale.Negotiate(w, r))
	})
}

// Delete serves DELETE /v1/listings/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in listingRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Delete(r.Context(), p.AccountID, in.ID)
	})
}

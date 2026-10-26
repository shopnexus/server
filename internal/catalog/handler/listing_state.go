package handler

import (
	"context"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
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
	change func(context.Context, uuid.UUID, uuid.UUID) (service.ListingView, error),
) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := change(r.Context(), p.AccountID, in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// Delete serves DELETE /v1/listings/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Delete(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

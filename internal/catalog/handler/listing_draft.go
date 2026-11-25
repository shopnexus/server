package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// putDraftRequest is the ListingDraftInput schema with the draft's ID.
type putDraftRequest struct {
	ID            uuid.UUID   `json:"-"              path:"id"`
	SchemaVersion int         `json:"schema_version"`
	Title         string      `json:"title"`
	Price         *int64      `json:"price"`
	Photos        []uuid.UUID `json:"photos"`
	Body          string      `json:"body"`
}

// PutDraft serves PUT /v1/listing-drafts/{id}: 201 for a create, 200 for a
// replace.
func (h *Handler) PutDraft(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in putDraftRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, created, err := h.svc.PutDraft(r.Context(), p.AccountID, in.ID, domain.DraftContent{
		SchemaVersion: in.SchemaVersion, Title: in.Title, Price: in.Price, Photos: in.Photos, Body: in.Body,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.Respond(w, status, v)
}

// ListDrafts serves GET /v1/listing-drafts.
func (h *Handler) ListDrafts(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (list[service.DraftView], error) {
		items, err := h.svc.ListDrafts(r.Context(), p.AccountID)
		return list[service.DraftView]{Items: items}, err
	})
}

// GetDraft serves GET /v1/listing-drafts/{id}.
func (h *Handler) GetDraft(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in listingRequest) (service.DraftView, error) {
		return h.svc.GetDraft(r.Context(), p.AccountID, in.ID)
	})
}

// DeleteDraft serves DELETE /v1/listing-drafts/{id}.
func (h *Handler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in listingRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteDraft(r.Context(), p.AccountID, in.ID)
	})
}

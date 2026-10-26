package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Get serves GET /v1/listings/{id}, signed in or not.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	var viewer *uuid.UUID
	if p, ok := authn.From(r.Context()); ok {
		viewer = &p.AccountID
	}
	v, err := h.svc.Get(r.Context(), viewer, in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

type listMineRequest struct {
	page.Request

	Pane string `json:"-" query:"pane"`
}

// ListMine serves GET /v1/me/listings.
func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listMineRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.ListMine(r.Context(), p.AccountID, service.Pane(in.Pane), in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

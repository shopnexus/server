// Package handler serves module discovery's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the discovery endpoints. The saved, hidden and viewed ones
// and the search history are behind protect; search and a seller's
// listings read a caller when identify finds one, and limitGuests rate
// limits a guest's search with q, the one that embeds its words.
func (h *Handler) Register(r *httpx.Router, protect, identify, limitGuests func(http.Handler) http.Handler) {
	guest := limitGuests(http.HandlerFunc(h.Search))
	r.Handle("GET /v1/search", identify(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if httpx.Viewer(req) == nil && req.URL.Query().Get("q") != "" {
			guest.ServeHTTP(w, req)
			return
		}
		h.Search(w, req)
	})))
	r.Handle("GET /v1/sellers/{id}/listings", identify(http.HandlerFunc(h.SellerListings)))
	r.Handle("POST /v1/search/watches", protect(http.HandlerFunc(h.Watch)))
	r.Handle("GET /v1/search/watches", protect(http.HandlerFunc(h.Watches)))
	r.Handle("DELETE /v1/search/watches/{id}", protect(http.HandlerFunc(h.Unwatch)))
	r.Handle("POST /v1/search/watches/{id}/mute", protect(http.HandlerFunc(h.Mute)))
	r.Handle("POST /v1/search/watches/{id}/unmute", protect(http.HandlerFunc(h.Unmute)))
	r.Handle("GET /v1/search/history", protect(http.HandlerFunc(h.History)))
	r.Handle("DELETE /v1/search/history", protect(http.HandlerFunc(h.ClearHistory)))
	r.Handle("DELETE /v1/search/history/{id}", protect(http.HandlerFunc(h.ForgetSearch)))
	r.Handle("GET /v1/saved", protect(http.HandlerFunc(h.Saved)))
	r.Handle("PUT /v1/saved/{listing_id}", protect(http.HandlerFunc(h.Save)))
	r.Handle("DELETE /v1/saved/{listing_id}", protect(http.HandlerFunc(h.Unsave)))
	r.Handle("PUT /v1/saved/{listing_id}/alert", protect(http.HandlerFunc(h.SetAlert)))
	r.Handle("DELETE /v1/saved/{listing_id}/alert", protect(http.HandlerFunc(h.ClearAlert)))
	r.Handle("PUT /v1/hidden/{listing_id}", protect(http.HandlerFunc(h.Hide)))
	r.Handle("DELETE /v1/hidden/{listing_id}", protect(http.HandlerFunc(h.Unhide)))
	r.Handle("PUT /v1/viewed/{listing_id}", protect(http.HandlerFunc(h.MarkViewed)))
}

type listingRequest struct {
	ListingID uuid.UUID `json:"-" path:"listing_id"`
}

// Save serves PUT /v1/saved/{listing_id}.
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Save(r.Context(), p.AccountID, in.ListingID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unsave serves DELETE /v1/saved/{listing_id}.
func (h *Handler) Unsave(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Unsave(r.Context(), p.AccountID, in.ListingID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Saved serves GET /v1/saved.
func (h *Handler) Saved(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in page.Request
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Saved(r.Context(), p.AccountID, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

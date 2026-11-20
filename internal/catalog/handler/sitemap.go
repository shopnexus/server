package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// sitemapCache lets the website and any proxy keep a sitemap answer an hour.
const sitemapCache = "public, max-age=3600"

type sitemapPartRequest struct {
	Cursor uuid.UUID `json:"-" query:"cursor"`
}

// Sitemap serves GET /v1/sitemap.
func (h *Handler) Sitemap(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Sitemap(r.Context())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.Header().Set("Cache-Control", sitemapCache)
	httpx.Respond(w, http.StatusOK, v)
}

// SitemapListings serves GET /v1/sitemap/listings.
func (h *Handler) SitemapListings(w http.ResponseWriter, r *http.Request) {
	var in sitemapPartRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.SitemapListings(r.Context(), in.Cursor)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.Header().Set("Cache-Control", sitemapCache)
	httpx.Respond(w, http.StatusOK, list[service.SitemapListingView]{Items: v})
}

// SitemapSellers serves GET /v1/sitemap/sellers.
func (h *Handler) SitemapSellers(w http.ResponseWriter, r *http.Request) {
	var in sitemapPartRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.SitemapSellers(r.Context(), in.Cursor)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.Header().Set("Cache-Control", sitemapCache)
	httpx.Respond(w, http.StatusOK, list[service.SitemapSellerView]{Items: v})
}

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

// cached marks a successful sitemap answer cacheable and passes err on.
func cached(w http.ResponseWriter, err error) error {
	if err == nil {
		w.Header().Set("Cache-Control", sitemapCache)
	}
	return err
}

// Sitemap serves GET /v1/sitemap.
func (h *Handler) Sitemap(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(httpx.None) (service.SitemapView, error) {
		v, err := h.svc.Sitemap(r.Context())
		return v, cached(w, err)
	})
}

// SitemapListings serves GET /v1/sitemap/listings.
func (h *Handler) SitemapListings(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in sitemapPartRequest) (list[service.SitemapListingView], error) {
		v, err := h.svc.SitemapListings(r.Context(), in.Cursor)
		return list[service.SitemapListingView]{Items: v}, cached(w, err)
	})
}

// SitemapSellers serves GET /v1/sitemap/sellers.
func (h *Handler) SitemapSellers(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in sitemapPartRequest) (list[service.SitemapSellerView], error) {
		v, err := h.svc.SitemapSellers(r.Context(), in.Cursor)
		return list[service.SitemapSellerView]{Items: v}, cached(w, err)
	})
}

// SitemapAreas serves GET /v1/sitemap/areas.
func (h *Handler) SitemapAreas(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(httpx.None) (list[service.SitemapAreaView], error) {
		v, err := h.svc.SitemapAreas(r.Context())
		return list[service.SitemapAreaView]{Items: v}, cached(w, err)
	})
}

// SitemapCategories serves GET /v1/sitemap/categories.
func (h *Handler) SitemapCategories(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(httpx.None) (list[service.SitemapCategoryView], error) {
		v, err := h.svc.SitemapCategories(r.Context())
		return list[service.SitemapCategoryView]{Items: v}, cached(w, err)
	})
}

// indexNowKeyView is the IndexNowKey schema.
type indexNowKeyView struct {
	Key string `json:"key"`
}

// IndexNowKey serves GET /v1/sitemap/indexnow.
func (h *Handler) IndexNowKey(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(httpx.None) (indexNowKeyView, error) {
		key, err := h.svc.IndexNowKey(r.Context())
		return indexNowKeyView{Key: key}, cached(w, err)
	})
}

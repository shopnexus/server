// Package handler serves module catalog's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the catalog endpoints. protect requires a signed-in
// caller; identify lets anyone through and tells the handler who signed in.
func (h *Handler) Register(r *httpx.Router, protect, identify func(http.Handler) http.Handler) {
	r.Handle("POST /v1/listings", protect(http.HandlerFunc(h.Post)))
	r.Handle("GET /v1/listings/{id}", identify(http.HandlerFunc(h.Get)))
	r.Handle("PUT /v1/listings/{id}", protect(http.HandlerFunc(h.Update)))
	r.Handle("DELETE /v1/listings/{id}", protect(http.HandlerFunc(h.Delete)))
	r.Handle("POST /v1/listings/{id}/hide", protect(http.HandlerFunc(h.Hide)))
	r.Handle("POST /v1/listings/{id}/unhide", protect(http.HandlerFunc(h.Unhide)))
	r.Handle("POST /v1/listings/{id}/mark-sold", protect(http.HandlerFunc(h.MarkSold)))
	r.Handle("POST /v1/listings/{id}/unmark-sold", protect(http.HandlerFunc(h.UnmarkSold)))
	r.Handle("GET /v1/me/listings", protect(http.HandlerFunc(h.ListMine)))
	r.Handle("GET /v1/categories", http.HandlerFunc(h.Categories))
	r.Handle("GET /v1/regions", http.HandlerFunc(h.Provinces))
	r.Handle("GET /v1/regions/{province_code}/communes", http.HandlerFunc(h.Communes))
}

// list is the body of a list response that has no pages.
type list[T any] struct {
	Items []T `json:"items"`
}

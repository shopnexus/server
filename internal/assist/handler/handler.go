// Package handler serves module assist's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/assist/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the assist endpoints; protect requires a signed-in caller.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/listing-reads", protect(http.HandlerFunc(h.StartRead)))
	r.Handle("GET /v1/listing-reads/{id}", protect(http.HandlerFunc(h.GetRead)))
	r.Handle("POST /v1/link-previews", protect(http.HandlerFunc(h.PreviewLink)))
}

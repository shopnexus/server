// Package handler serves module media's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the media endpoints behind protect, which requires a
// signed-in caller.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/uploads", protect(http.HandlerFunc(h.CreateUpload)))
}

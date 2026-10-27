// Package handler serves module chat's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the chat endpoints, all behind protect.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/conversations", protect(http.HandlerFunc(h.Open)))
	r.Handle("GET /v1/conversations/{id}/messages", protect(http.HandlerFunc(h.Messages)))
	r.Handle("POST /v1/conversations/{id}/messages", protect(http.HandlerFunc(h.Send)))
	r.Handle("POST /v1/conversations/{id}/read", protect(http.HandlerFunc(h.Read)))
	r.Handle("POST /v1/conversations/{id}/archive", protect(http.HandlerFunc(h.Archive)))
	r.Handle("POST /v1/conversations/{id}/unarchive", protect(http.HandlerFunc(h.Unarchive)))
	r.Handle("GET /v1/me/unread", protect(http.HandlerFunc(h.Unread)))
}

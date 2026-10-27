// Package handler serves module screen's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/screen/service"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the screen endpoints. protect requires a signed-in caller;
// identify reads one when the request carries it.
func (h *Handler) Register(r *httpx.Router, protect, identify func(http.Handler) http.Handler) {
	r.Handle("GET /v1/screens/home", identify(http.HandlerFunc(h.Home)))
	r.Handle("GET /v1/screens/listing/{id}", identify(http.HandlerFunc(h.Listing)))
	r.Handle("GET /v1/screens/seller/{id}", identify(http.HandlerFunc(h.Seller)))
	r.Handle("GET /v1/screens/inbox", protect(http.HandlerFunc(h.Inbox)))
	r.Handle("GET /v1/screens/chat/{conversation_id}", protect(http.HandlerFunc(h.Chat)))
}

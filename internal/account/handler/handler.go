// Package handler serves module account's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the account endpoints behind protect, which requires a
// signed-in caller.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("GET /v1/accounts/me", protect(http.HandlerFunc(h.GetMe)))
	r.Handle("PATCH /v1/accounts/me", protect(http.HandlerFunc(h.Rename)))
	r.Handle("POST /v1/blocks", protect(http.HandlerFunc(h.Block)))
	r.Handle("GET /v1/blocks", protect(http.HandlerFunc(h.Blocks)))
	r.Handle("DELETE /v1/blocks/{account_id}", protect(http.HandlerFunc(h.Unblock)))
}

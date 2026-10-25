// Package handler serves module auth's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the auth endpoints; protect requires a signed-in caller.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/auth/otp", http.HandlerFunc(h.SendOTP))
	r.Handle("POST /v1/auth/otp/verify", http.HandlerFunc(h.SignIn))
	r.Handle("POST /v1/auth/refresh", http.HandlerFunc(h.Refresh))
	r.Handle("POST /v1/auth/logout", protect(http.HandlerFunc(h.Logout)))
	r.Handle("POST /v1/auth/logout-all", protect(http.HandlerFunc(h.LogoutAll)))
	r.Handle("GET /v1/auth/sessions", protect(http.HandlerFunc(h.ListSessions)))
	r.Handle("DELETE /v1/auth/sessions/{id}", protect(http.HandlerFunc(h.RemoveSession)))
}

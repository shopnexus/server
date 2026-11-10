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

// Register adds the account and sign-in endpoints; protect requires a
// signed-in caller.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/auth/sign-in", http.HandlerFunc(h.SignIn))
	r.Handle("POST /v1/auth/refresh", http.HandlerFunc(h.Refresh))
	r.Handle("GET /v1/auth/sessions", protect(http.HandlerFunc(h.ListSessions)))
	r.Handle("DELETE /v1/auth/sessions", protect(http.HandlerFunc(h.DeleteAllSessions)))
	// "current" is more specific than {id}: ServeMux and the request
	// validator's router both match it first (TestCurrentSessionRoute).
	r.Handle("DELETE /v1/auth/sessions/current", protect(http.HandlerFunc(h.DeleteCurrentSession)))
	r.Handle("DELETE /v1/auth/sessions/{id}", protect(http.HandlerFunc(h.DeleteSession)))
	r.Handle("GET /v1/accounts/me", protect(http.HandlerFunc(h.GetMe)))
	r.Handle("PATCH /v1/accounts/me", protect(http.HandlerFunc(h.Update)))
	r.Handle("POST /v1/blocks", protect(tie[accountBody](h, h.svc.Block)))
	r.Handle("GET /v1/blocks", protect(ties(h, h.svc.Blocks)))
	r.Handle("DELETE /v1/blocks/{account_id}", protect(tie[accountPath](h, h.svc.Unblock)))
	r.Handle("POST /v1/follows", protect(tie[accountBody](h, h.svc.Follow)))
	r.Handle("GET /v1/follows", protect(ties(h, h.svc.Follows)))
	r.Handle("DELETE /v1/follows/{account_id}", protect(tie[accountPath](h, h.svc.Unfollow)))
	r.Handle("GET /v1/addresses", protect(http.HandlerFunc(h.Addresses)))
	r.Handle("POST /v1/addresses", protect(http.HandlerFunc(h.CreateAddress)))
	r.Handle("PUT /v1/addresses/{id}", protect(http.HandlerFunc(h.UpdateAddress)))
	r.Handle("DELETE /v1/addresses/{id}", protect(http.HandlerFunc(h.DeleteAddress)))
	r.Handle("POST /v1/addresses/{id}/default", protect(http.HandlerFunc(h.SetDefaultAddress)))
}

// Package handler serves module erasure's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server/internal/erasure/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds DELETE /v1/accounts/me behind protect.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("DELETE /v1/accounts/me", protect(http.HandlerFunc(h.DeleteAccount)))
}

// DeleteAccount serves DELETE /v1/accounts/me.
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, _ httpx.None) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteAccount(r.Context(), p)
	})
}

// Package handler serves module notification's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the notification endpoints, all behind protect.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("GET /v1/notifications", protect(http.HandlerFunc(h.List)))
	r.Handle("POST /v1/notifications/read-all", protect(http.HandlerFunc(h.ReadAll)))
	r.Handle("POST /v1/notifications/{id}/read", protect(http.HandlerFunc(h.Read)))
	r.Handle("PUT /v1/me/push-token", protect(http.HandlerFunc(h.PutToken)))
	r.Handle("DELETE /v1/me/push-token", protect(http.HandlerFunc(h.DeleteToken)))
}

// List serves GET /v1/notifications.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in page.Request
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.List(r.Context(), p.AccountID, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type readRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// Read serves POST /v1/notifications/{id}/read.
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in readRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Read(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReadAll serves POST /v1/notifications/read-all.
func (h *Handler) ReadAll(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	if err := h.svc.ReadAll(r.Context(), p.AccountID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tokenRequest struct {
	Token string `json:"token"`
}

// PutToken serves PUT /v1/me/push-token.
func (h *Handler) PutToken(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in tokenRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.PutToken(r.Context(), p, in.Token); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteToken serves DELETE /v1/me/push-token.
func (h *Handler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	if err := h.svc.DeleteToken(r.Context(), p); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

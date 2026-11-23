// Package handler serves module notification's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
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
	r.Handle("POST /v1/notifications/unread", protect(http.HandlerFunc(h.MarkUnread)))
	r.Handle("POST /v1/notifications/{id}/read", protect(http.HandlerFunc(h.Read)))
	r.Handle("PUT /v1/me/push-token", protect(http.HandlerFunc(h.PutToken)))
	r.Handle("DELETE /v1/me/push-token", protect(http.HandlerFunc(h.DeleteToken)))
	r.Handle("GET /v1/me/notification-settings", protect(http.HandlerFunc(h.Settings)))
	r.Handle("PUT /v1/me/notification-settings", protect(http.HandlerFunc(h.PutSettings)))
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
	out, err := h.svc.List(r.Context(), p.AccountID, in, locale.FromRequest(r))
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

type readAllResponse struct {
	IDs []uuid.UUID `json:"ids"`
}

// ReadAll serves POST /v1/notifications/read-all.
func (h *Handler) ReadAll(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	ids, err := h.svc.ReadAll(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, readAllResponse{IDs: ids})
}

type unreadRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

// MarkUnread serves POST /v1/notifications/unread.
func (h *Handler) MarkUnread(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in unreadRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.MarkUnread(r.Context(), p.AccountID, in.IDs); err != nil {
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
	if err := h.svc.PutToken(r.Context(), p.SessionID, p.AccountID, in.Token); err != nil {
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
	if err := h.svc.DeleteToken(r.Context(), p.SessionID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Package handler serves module notification's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/platform/authn"
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
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in page.Request) (service.List, error) {
		return h.svc.List(r.Context(), p.AccountID, in, locale.Negotiate(w, r))
	})
}

type readRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// Read serves POST /v1/notifications/{id}/read.
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in readRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Read(r.Context(), p.AccountID, in.ID)
	})
}

type readAllResponse struct {
	IDs []uuid.UUID `json:"ids"`
}

// ReadAll serves POST /v1/notifications/read-all.
func (h *Handler) ReadAll(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (readAllResponse, error) {
		ids, err := h.svc.ReadAll(r.Context(), p.AccountID)
		return readAllResponse{IDs: ids}, err
	})
}

type unreadRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

// MarkUnread serves POST /v1/notifications/unread.
func (h *Handler) MarkUnread(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in unreadRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.MarkUnread(r.Context(), p.AccountID, in.IDs)
	})
}

type tokenRequest struct {
	Token string `json:"token"`
}

// PutToken serves PUT /v1/me/push-token.
func (h *Handler) PutToken(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in tokenRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.PutToken(r.Context(), p.SessionID, p.AccountID, in.Token)
	})
}

// DeleteToken serves DELETE /v1/me/push-token.
func (h *Handler) DeleteToken(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, _ httpx.None) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteToken(r.Context(), p.SessionID)
	})
}

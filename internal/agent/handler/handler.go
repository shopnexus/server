// Package handler serves the console's agent endpoints under
// /v1/console/agent, which forward to the agent service.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/agent/domain"
	"github.com/shopnexus/server2/internal/agent/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the agent endpoints behind moderator, console's guard of a
// moderator's or an admin's route.
func (h *Handler) Register(r *httpx.Router, moderator func(http.Handler) http.Handler) {
	r.Handle("GET /v1/console/agent/conversations", moderator(http.HandlerFunc(h.ListConversations)))
	r.Handle("POST /v1/console/agent/conversations", moderator(http.HandlerFunc(h.CreateConversation)))
	r.Handle("GET /v1/console/agent/conversations/{id}", moderator(http.HandlerFunc(h.GetConversation)))
	r.Handle("POST /v1/console/agent/conversations/{id}/turns", moderator(http.HandlerFunc(h.Ask)))
}

type listRequest struct {
	SubjectKind domain.SubjectKind `json:"-" query:"subject_kind"`
	SubjectID   *uuid.UUID         `json:"-" query:"subject_id"`
}

// ListConversations serves GET /v1/console/agent/conversations.
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in listRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Conversations(r.Context(), caller, in.SubjectKind, in.SubjectID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, map[string][]service.Conversation{"items": out})
}

type createRequest struct {
	SubjectKind domain.SubjectKind `json:"subject_kind"`
	SubjectID   *uuid.UUID         `json:"subject_id"`
}

// CreateConversation serves POST /v1/console/agent/conversations.
func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in createRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.CreateConversation(r.Context(), caller, service.NewConversation(in))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type conversationRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetConversation serves GET /v1/console/agent/conversations/{id}.
func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in conversationRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Conversation(r.Context(), caller, in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type askRequest struct {
	ID   uuid.UUID `json:"-"    path:"id"`
	Text string    `json:"text"`
}

// Ask serves POST /v1/console/agent/conversations/{id}/turns.
func (h *Handler) Ask(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in askRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Ask(r.Context(), caller, in.ID, in.Text)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

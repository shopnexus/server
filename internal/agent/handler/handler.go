// Package handler serves the console's agent endpoints under
// /v1/console/agent, the console agents a member may pick, the AI settings
// under /v1/console/settings, which the server writes into Bifrost, and the
// gate that lets an agent's turn call the console API with its token.
package handler

import (
	"log/slog"
	"net/http"
	"sync/atomic"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
	// root is the server's whole handler, which an approved request goes
	// through as if the approver made it; the app sets it once built.
	root atomic.Pointer[http.Handler]
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// SetRoot hands the handler the server's handler, for approvals.
func (h *Handler) SetRoot(root http.Handler) { h.root.Store(&root) }

// Register adds the agent endpoints behind moderator, console's guard of a
// moderator's or an admin's route, and the AI settings behind admin.
func (h *Handler) Register(r *httpx.Router, moderator, admin func(http.Handler) http.Handler) {
	mod := func(pattern string, f http.HandlerFunc) { r.Handle(pattern, moderator(f)) }
	adm := func(pattern string, f http.HandlerFunc) { r.Handle(pattern, admin(f)) }
	mod("GET /v1/console/agent/conversations", h.ListConversations)
	mod("POST /v1/console/agent/conversations", h.CreateConversation)
	mod("GET /v1/console/agent/conversations/{id}", h.GetConversation)
	mod("PATCH /v1/console/agent/conversations/{id}", h.RenameConversation)
	mod("DELETE /v1/console/agent/conversations/{id}", h.DeleteConversation)
	mod("POST /v1/console/agent/conversations/{id}/turns", h.Ask)
	mod("GET /v1/console/agent/turns/{id}", h.GetTurn)
	mod("GET /v1/console/agent/turns/{id}/events", h.Events)
	mod("POST /v1/console/agent/turns/{id}/pause", h.Pause)
	mod("POST /v1/console/agent/turns/{id}/stop", h.Stop)
	mod("POST /v1/console/agent/turns/{id}/resume", h.Resume)
	mod("POST /v1/console/agent/turns/{id}/steer", h.Steer)
	mod("GET /v1/console/agent/operations", h.Operations)
	mod("GET /v1/console/agent/approvals", h.Approvals)
	mod("POST /v1/console/agent/approvals/{id}/approve", h.Approve)
	mod("POST /v1/console/agent/approvals/{id}/reject", h.Reject)
	mod("GET /v1/console/agents", h.ConsoleAgents)
	mod("GET /v1/console/orders/{id}", h.GetOrder)
	mod("GET /v1/console/listings/{id}", h.GetListing)

	adm("GET /v1/console/settings/providers", h.ListProviders)
	adm("GET /v1/console/settings/providers/{name}/models", h.ProviderModels)
	adm("GET /v1/console/settings/models", h.ListModels)
	adm("PUT /v1/console/settings/models/tags", h.SetModelTags)
	adm("PUT /v1/console/settings/models/price", h.SetModelPrice)
	adm("GET /v1/console/settings/agents", h.ListAgents)
	adm("POST /v1/console/settings/agents", h.CreateAgent)
	adm("PATCH /v1/console/settings/agents/{id}", h.UpdateAgent)
	adm("GET /v1/console/settings/agents/{id}/versions", h.AgentVersions)
	adm("GET /v1/console/settings/spend", h.GetSpend)
	adm("GET /v1/console/settings/agents/usage", h.AgentUsage)
}

// items is a list response, {items}.
type items[T any] struct {
	Items []T `json:"items"`
}

// listOf is a use case's list as a list response.
func listOf[T any](out []T, err error) (items[T], error) { return items[T]{out}, err }

type idRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

type listRequest struct {
	SubjectKind domain.SubjectKind `json:"-" query:"subject_kind"`
	SubjectID   *uuid.UUID         `json:"-" query:"subject_id"`
}

// ListConversations serves GET /v1/console/agent/conversations.
func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in listRequest) (items[service.Conversation], error) {
		return listOf(h.svc.Conversations(r.Context(), c, service.ConversationQuery(in)))
	})
}

type createRequest struct {
	SubjectKind domain.SubjectKind `json:"subject_kind"`
	SubjectID   *uuid.UUID         `json:"subject_id"`
	Key         *uuid.UUID         `header:"Idempotency-Key" json:"-"`
}

// CreateConversation serves POST /v1/console/agent/conversations.
func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in createRequest) (service.Conversation, error) {
		return h.svc.CreateConversation(r.Context(), c, service.NewConversation(in))
	})
}

// GetConversation serves GET /v1/console/agent/conversations/{id}.
func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in idRequest) (service.ConversationDetail, error) {
		return h.svc.Conversation(r.Context(), c, in.ID)
	})
}

type renameRequest struct {
	ID    uuid.UUID `json:"-"     path:"id"`
	Title string    `json:"title"`
}

// RenameConversation serves PATCH /v1/console/agent/conversations/{id}.
func (h *Handler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in renameRequest) (service.Conversation, error) {
		return h.svc.RenameConversation(r.Context(), c, in.ID, in.Title)
	})
}

// DeleteConversation serves DELETE /v1/console/agent/conversations/{id}.
func (h *Handler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusNoContent, func(c authn.ConsoleMember, in idRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.DeleteConversation(r.Context(), c, in.ID)
	})
}

type askRequest struct {
	ID      uuid.UUID  `json:"-"                 path:"id"`
	Key     *uuid.UUID `header:"Idempotency-Key" json:"-"`
	Text    string     `json:"text"`
	AgentID *uuid.UUID `json:"agent_id"`
}

// Ask serves POST /v1/console/agent/conversations/{id}/turns: 202 with
// the turn, whose run the console follows on its events.
func (h *Handler) Ask(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusAccepted, func(c authn.ConsoleMember, in askRequest) (service.TurnStarted, error) {
		return h.svc.Ask(r.Context(), c, in.ID, service.NewTurn{Text: in.Text, AgentID: in.AgentID, Key: in.Key})
	})
}

// GetTurn serves GET /v1/console/agent/turns/{id}.
func (h *Handler) GetTurn(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(c authn.ConsoleMember, in idRequest) (service.Turn, error) {
		return h.svc.Turn(r.Context(), c, in.ID)
	})
}

// Pause serves POST /v1/console/agent/turns/{id}/pause.
func (h *Handler) Pause(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusAccepted, func(c authn.ConsoleMember, in idRequest) (service.TurnState, error) {
		return h.svc.Pause(r.Context(), c, in.ID)
	})
}

// Stop serves POST /v1/console/agent/turns/{id}/stop.
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusAccepted, func(c authn.ConsoleMember, in idRequest) (service.TurnState, error) {
		return h.svc.Stop(r.Context(), c, in.ID)
	})
}

// Resume serves POST /v1/console/agent/turns/{id}/resume.
func (h *Handler) Resume(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusAccepted, func(c authn.ConsoleMember, in idRequest) (service.TurnState, error) {
		return h.svc.Resume(r.Context(), c, in.ID)
	})
}

type steerRequest struct {
	ID   uuid.UUID `json:"-"    path:"id"`
	Text string    `json:"text"`
}

// Steer serves POST /v1/console/agent/turns/{id}/steer.
func (h *Handler) Steer(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusAccepted, func(c authn.ConsoleMember, in steerRequest) (service.TurnState, error) {
		return h.svc.Steer(r.Context(), c, in.ID, in.Text)
	})
}

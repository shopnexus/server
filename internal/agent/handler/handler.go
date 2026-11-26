// Package handler serves the console's agent endpoints under
// /v1/console/agent, the console agents a member may pick, and the AI
// settings under /v1/console/settings, which the server writes into
// Bifrost.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/agent/domain"
	"github.com/shopnexus/server/internal/agent/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

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
	for _, c := range []service.Control{service.ControlPause, service.ControlStop, service.ControlContinue, service.ControlEnd} {
		mod("POST /v1/console/agent/turns/{id}/"+string(c), h.control(c))
	}
	mod("POST /v1/console/agent/turns/{id}/steer", h.Steer)
	mod("POST /v1/console/agent/turns/{id}/input", h.Input)
	mod("GET /v1/console/agent/tool-calls/{id}", h.ToolCall)
	mod("GET /v1/console/agent/documents/{id}", h.Document)
	mod("GET /v1/console/agent/skills", h.ListSkills)
	mod("POST /v1/console/agent/skills", h.CreateSkill)
	mod("GET /v1/console/agent/skills/{id}", h.GetSkill)
	mod("PATCH /v1/console/agent/skills/{id}", h.UpdateSkill)
	mod("DELETE /v1/console/agent/skills/{id}", h.DeleteSkill)
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
	adm("GET /v1/console/settings/tools", h.Tools)
	adm("GET /v1/console/settings/guardrails", h.Guardrails)
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
	MentionKind string             `json:"-" query:"mention_kind"`
	MentionID   *uuid.UUID         `json:"-" query:"mention_id"`
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
	service.NewTurn

	ID  uuid.UUID  `json:"-"                 path:"id"`
	Key *uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// Ask serves POST /v1/console/agent/conversations/{id}/turns: 202 with
// the turn, whose run the console follows on its events.
func (h *Handler) Ask(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusAccepted, func(c authn.ConsoleMember, in askRequest) (service.TurnStarted, error) {
		in.NewTurn.Key = in.Key
		return h.svc.Ask(r.Context(), c, in.ID, in.NewTurn)
	})
}

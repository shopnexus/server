package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
	supportdomain "github.com/shopnexus/server/internal/support/domain"
	supportsvc "github.com/shopnexus/server/internal/support/service"
)

type queueRequest struct {
	Status string    `json:"-" query:"status"`
	Kind   string    `json:"-" query:"kind"`
	Cursor uuid.UUID `json:"-" query:"cursor"`
	Limit  int       `json:"-" query:"limit"`
}

// ListTickets serves GET /v1/console/support/tickets.
func (h *Handler) ListTickets(w http.ResponseWriter, r *http.Request) {
	var in queueRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	var kind *supportdomain.Kind
	if in.Kind != "" {
		kind = new(supportdomain.Kind(in.Kind))
	}
	out, err := h.support.Queue(r.Context(), supportdomain.Status(in.Status), kind, page.Request{Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type ticketRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetTicket serves GET /v1/console/support/tickets/{id}.
func (h *Handler) GetTicket(w http.ResponseWriter, r *http.Request) {
	var in ticketRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.support.Detail(r.Context(), in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type ticketMessagesRequest struct {
	ID     uuid.UUID `json:"-" path:"id"`
	Cursor uuid.UUID `json:"-" query:"cursor"`
	Limit  int       `json:"-" query:"limit"`
}

// ListTicketMessages serves GET /v1/console/support/tickets/{id}/messages.
func (h *Handler) ListTicketMessages(w http.ResponseWriter, r *http.Request) {
	var in ticketMessagesRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.support.Messages(r.Context(), in.ID, page.Request{Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type replyRequest struct {
	ID      uuid.UUID  `json:"-"                 path:"id"`
	Key     uuid.UUID  `header:"Idempotency-Key" json:"-"`
	Body    string     `json:"body"`
	ReplyTo *uuid.UUID `json:"reply_to"`
}

// ReplyTicket serves POST /v1/console/support/tickets/{id}/replies.
func (h *Handler) ReplyTicket(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in replyRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.support.Reply(r.Context(), in.ID, in.Key.String(), supportsvc.ReplyInput{Body: in.Body, ReplyTo: in.ReplyTo},
		audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, out)
}

// ResolveTicket serves POST /v1/console/support/tickets/{id}/resolve.
func (h *Handler) ResolveTicket(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in ticketRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.support.Resolve(r.Context(), in.ID, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// ReopenTicket serves POST /v1/console/support/tickets/{id}/reopen.
func (h *Handler) ReopenTicket(w http.ResponseWriter, r *http.Request) {
	caller, ok := httpx.ConsoleCaller(w, r, h.log)
	if !ok {
		return
	}
	var in ticketRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.support.Reopen(r.Context(), in.ID, audit.ByMember(caller))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// SupportSummary serves GET /v1/console/support/summary.
func (h *Handler) SupportSummary(w http.ResponseWriter, r *http.Request) {
	n, err := h.support.OpenCount(r.Context())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, map[string]int{"open": n})
}

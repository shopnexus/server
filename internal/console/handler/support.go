package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/authn"
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
	httpx.Serve(w, r, h.log, http.StatusOK, func(in queueRequest) (page.Page[supportsvc.ConsoleTicket], error) {
		var kind *supportdomain.Kind
		if in.Kind != "" {
			kind = new(supportdomain.Kind(in.Kind))
		}
		return h.support.Queue(r.Context(), supportdomain.Status(in.Status), kind, page.Request{Cursor: in.Cursor, Limit: in.Limit})
	})
}

type ticketRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetTicket serves GET /v1/console/support/tickets/{id}.
func (h *Handler) GetTicket(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in ticketRequest) (supportsvc.ConsoleTicketDetail, error) {
		return h.support.Detail(r.Context(), in.ID)
	})
}

type ticketMessagesRequest struct {
	ID     uuid.UUID `json:"-" path:"id"`
	Cursor uuid.UUID `json:"-" query:"cursor"`
	Limit  int       `json:"-" query:"limit"`
}

// ListTicketMessages serves GET /v1/console/support/tickets/{id}/messages.
func (h *Handler) ListTicketMessages(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in ticketMessagesRequest) (page.Page[supportsvc.ConsoleMessage], error) {
		return h.support.Messages(r.Context(), in.ID, page.Request{Cursor: in.Cursor, Limit: in.Limit})
	})
}

type replyRequest struct {
	ID      uuid.UUID  `json:"-"                 path:"id"`
	Key     uuid.UUID  `header:"Idempotency-Key" json:"-"`
	Body    string     `json:"body"`
	ReplyTo *uuid.UUID `json:"reply_to"`
}

// ReplyTicket serves POST /v1/console/support/tickets/{id}/replies.
func (h *Handler) ReplyTicket(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusCreated, func(caller authn.ConsoleMember, in replyRequest) (supportsvc.ConsoleMessage, error) {
		return h.support.Reply(r.Context(), in.ID, in.Key, supportsvc.ReplyInput{Body: in.Body, ReplyTo: in.ReplyTo},
			audit.ByMember(caller))
	})
}

// ResolveTicket serves POST /v1/console/support/tickets/{id}/resolve.
func (h *Handler) ResolveTicket(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in ticketRequest) (supportsvc.ConsoleTicket, error) {
		return h.support.Resolve(r.Context(), in.ID, audit.ByMember(caller))
	})
}

// ReopenTicket serves POST /v1/console/support/tickets/{id}/reopen.
func (h *Handler) ReopenTicket(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(caller authn.ConsoleMember, in ticketRequest) (supportsvc.ConsoleTicket, error) {
		return h.support.Reopen(r.Context(), in.ID, audit.ByMember(caller))
	})
}

// SupportSummary serves GET /v1/console/support/summary.
func (h *Handler) SupportSummary(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(_ httpx.None) (map[string]int, error) {
		n, err := h.support.OpenCount(r.Context())
		return map[string]int{"open": n}, err
	})
}

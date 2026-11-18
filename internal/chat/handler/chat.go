package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type openRequest struct {
	IdempotencyKey string      `header:"Idempotency-Key" json:"-"`
	ListingID      uuid.UUID   `json:"listing_id"`
	Text           string      `json:"text"`
	Photos         []uuid.UUID `json:"photos"`
}

type conversationRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

type sendRequest struct {
	ID             uuid.UUID   `json:"-"                 path:"id"`
	IdempotencyKey string      `header:"Idempotency-Key" json:"-"`
	Text           string      `json:"text"`
	Photos         []uuid.UUID `json:"photos"`
	ListingID      *uuid.UUID  `json:"listing_id"`
	ReplyTo        *uuid.UUID  `json:"reply_to"`
}

type readRequest struct {
	ID        uuid.UUID `json:"-"          path:"id"`
	MessageID uuid.UUID `json:"message_id"`
}

type messagesRequest struct {
	page.Request

	ID uuid.UUID `json:"-" path:"id"`
}

type unread struct {
	Unread int `json:"unread"`
}

// Open serves POST /v1/conversations.
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in openRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Open(r.Context(), p.AccountID, in.IdempotencyKey, service.OpenInput{
		ListingID: in.ListingID, Text: in.Text, Photos: in.Photos,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, v)
}

// Messages serves GET /v1/conversations/{id}/messages.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in messagesRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Messages(r.Context(), p.AccountID, in.ID, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// Send serves POST /v1/conversations/{id}/messages.
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in sendRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Send(r.Context(), p.AccountID, in.ID, in.IdempotencyKey, service.SendInput{
		Text: in.Text, Photos: in.Photos, ListingID: in.ListingID, ReplyTo: in.ReplyTo,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, v)
}

// Read serves POST /v1/conversations/{id}/read.
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
	if err := h.svc.Read(r.Context(), p.AccountID, in.ID, in.MessageID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MarkUnread serves POST /v1/conversations/{id}/unread.
func (h *Handler) MarkUnread(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in conversationRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.MarkUnread(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Archive serves POST /v1/conversations/{id}/archive.
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in conversationRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Archive(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unarchive serves POST /v1/conversations/{id}/unarchive.
func (h *Handler) Unarchive(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in conversationRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Unarchive(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unread serves GET /v1/me/unread.
func (h *Handler) Unread(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	n, err := h.svc.Unread(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, unread{Unread: n})
}

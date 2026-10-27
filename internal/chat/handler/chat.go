package handler

import (
	"context"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
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

// bind reads the caller and the request into in, answering the failure
// itself.
func bind[T any](h *Handler, w http.ResponseWriter, r *http.Request) (uuid.UUID, T, bool) {
	var in T
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return uuid.UUID{}, in, false
	}
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return uuid.UUID{}, in, false
	}
	return p.AccountID, in, true
}

// answer writes v with status, or err.
func answer[T any](h *Handler, w http.ResponseWriter, r *http.Request, status int, v T, err error) {
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, status, v)
}

// done writes 204, or err.
func (h *Handler) done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Open serves POST /v1/conversations.
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	caller, in, ok := bind[openRequest](h, w, r)
	if !ok {
		return
	}
	v, err := h.svc.Open(r.Context(), caller, in.IdempotencyKey, service.OpenInput{
		ListingID: in.ListingID, Text: in.Text, Photos: in.Photos,
	})
	answer(h, w, r, http.StatusCreated, v, err)
}

// Messages serves GET /v1/conversations/{id}/messages.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	caller, in, ok := bind[messagesRequest](h, w, r)
	if !ok {
		return
	}
	v, err := h.svc.Messages(r.Context(), caller, in.ID, in.Request)
	answer(h, w, r, http.StatusOK, v, err)
}

// Send serves POST /v1/conversations/{id}/messages.
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	caller, in, ok := bind[sendRequest](h, w, r)
	if !ok {
		return
	}
	v, err := h.svc.Send(r.Context(), caller, in.ID, in.IdempotencyKey, service.SendInput{
		Text: in.Text, Photos: in.Photos, ListingID: in.ListingID,
	})
	answer(h, w, r, http.StatusCreated, v, err)
}

// Read serves POST /v1/conversations/{id}/read.
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	caller, in, ok := bind[readRequest](h, w, r)
	if !ok {
		return
	}
	h.done(w, r, h.svc.Read(r.Context(), caller, in.ID, in.MessageID))
}

// Archive serves POST /v1/conversations/{id}/archive.
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	h.state(w, r, h.svc.Archive)
}

// Unarchive serves POST /v1/conversations/{id}/unarchive.
func (h *Handler) Unarchive(w http.ResponseWriter, r *http.Request) {
	h.state(w, r, h.svc.Unarchive)
}

// state serves archive and unarchive.
func (h *Handler) state(w http.ResponseWriter, r *http.Request, change func(context.Context, uuid.UUID, uuid.UUID) error) {
	caller, in, ok := bind[conversationRequest](h, w, r)
	if !ok {
		return
	}
	h.done(w, r, change(r.Context(), caller, in.ID))
}

// Unread serves GET /v1/me/unread.
func (h *Handler) Unread(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	n, err := h.svc.Unread(r.Context(), p.AccountID)
	answer(h, w, r, http.StatusOK, unread{Unread: n}, err)
}

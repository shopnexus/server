package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type openRequest struct {
	Key       uuid.UUID   `header:"Idempotency-Key" json:"-"`
	ListingID uuid.UUID   `json:"listing_id"`
	Text      string      `json:"text"`
	Photos    []uuid.UUID `json:"photos"`
}

type conversationRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

type sendRequest struct {
	ID        uuid.UUID   `json:"-"                 path:"id"`
	Key       uuid.UUID   `header:"Idempotency-Key" json:"-"`
	Text      string      `json:"text"`
	Photos    []uuid.UUID `json:"photos"`
	ListingID *uuid.UUID  `json:"listing_id"`
	ReplyTo   *uuid.UUID  `json:"reply_to"`
}

type readRequest struct {
	ID        uuid.UUID `json:"-"          path:"id"`
	MessageID uuid.UUID `json:"message_id"`
}

type messagesRequest struct {
	page.Request

	ID uuid.UUID `json:"-" path:"id"`
}

// Open serves POST /v1/conversations.
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusCreated, func(p authn.Principal, in openRequest) (service.ConversationView, error) {
		return h.svc.Open(r.Context(), p.AccountID, in.Key, service.OpenInput{
			ListingID: in.ListingID, Text: in.Text, Photos: in.Photos,
		})
	})
}

// Messages serves GET /v1/conversations/{id}/messages.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in messagesRequest) (page.Page[service.MessageView], error) {
		return h.svc.Messages(r.Context(), p.AccountID, in.ID, in.Request)
	})
}

// Send serves POST /v1/conversations/{id}/messages.
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusCreated, func(p authn.Principal, in sendRequest) (service.MessageView, error) {
		return h.svc.Send(r.Context(), p.AccountID, in.ID, in.Key, service.SendInput{
			Text: in.Text, Photos: in.Photos, ListingID: in.ListingID, ReplyTo: in.ReplyTo,
		})
	})
}

// Read serves POST /v1/conversations/{id}/read.
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in readRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Read(r.Context(), p.AccountID, in.ID, in.MessageID)
	})
}

// MarkUnread serves POST /v1/conversations/{id}/unread.
func (h *Handler) MarkUnread(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in conversationRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.MarkUnread(r.Context(), p.AccountID, in.ID)
	})
}

// Archive serves POST /v1/conversations/{id}/archive.
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in conversationRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Archive(r.Context(), p.AccountID, in.ID)
	})
}

// Unarchive serves POST /v1/conversations/{id}/unarchive.
func (h *Handler) Unarchive(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in conversationRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Unarchive(r.Context(), p.AccountID, in.ID)
	})
}

// Ignore serves POST /v1/conversations/{id}/ignore.
func (h *Handler) Ignore(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in conversationRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Ignore(r.Context(), p.AccountID, in.ID)
	})
}

// Unignore serves POST /v1/conversations/{id}/unignore.
func (h *Handler) Unignore(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in conversationRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.Unignore(r.Context(), p.AccountID, in.ID)
	})
}

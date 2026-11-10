// Package handler serves module support's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/support/service"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds POST /v1/feedback behind protect. Console members read
// feedback through console's handler.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/feedback", protect(http.HandlerFunc(h.Send)))
}

type sendRequest struct {
	Key  uuid.UUID `header:"Idempotency-Key" json:"-"`
	Body string    `json:"body"`
}

// Send serves POST /v1/feedback.
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
	out, err := h.svc.Send(r.Context(), p.AccountID, in.Key, in.Body)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, out)
}

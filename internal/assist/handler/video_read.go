package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type startRequest struct {
	Video          uuid.UUID `json:"video"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
}

// StartRead serves POST /v1/video-reads.
func (h *Handler) StartRead(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in startRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.StartRead(r.Context(), p.AccountID, in.IdempotencyKey, in.Video)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusAccepted, v)
}

type readRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetRead serves GET /v1/video-reads/{id}.
func (h *Handler) GetRead(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in readRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.GetRead(r.Context(), p.AccountID, in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

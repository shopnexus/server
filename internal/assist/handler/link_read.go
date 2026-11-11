package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type linkRequest struct {
	URL string `json:"url"`
}

// PreviewLink serves POST /v1/link-previews.
func (h *Handler) PreviewLink(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in linkRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.PreviewLink(r.Context(), p.AccountID, in.URL)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

type startLinkRequest struct {
	URL            string `json:"url"`
	IdempotencyKey string `header:"Idempotency-Key" json:"-"`
}

// StartLinkRead serves POST /v1/link-reads.
func (h *Handler) StartLinkRead(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in startLinkRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.StartLinkRead(r.Context(), p.AccountID, in.IdempotencyKey, in.URL)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusAccepted, v)
}

// GetLinkRead serves GET /v1/link-reads/{id}.
func (h *Handler) GetLinkRead(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in readRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.GetLinkRead(r.Context(), p.AccountID, in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
)

// History serves GET /v1/search/history.
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.History(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// ClearHistory serves DELETE /v1/search/history.
func (h *Handler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	if err := h.svc.ClearHistory(r.Context(), p.AccountID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type historyItemRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// ForgetSearch serves DELETE /v1/search/history/{id}.
func (h *Handler) ForgetSearch(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in historyItemRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.ForgetSearch(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

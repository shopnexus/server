package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
)

type followRequest struct {
	AccountID uuid.UUID `json:"account_id"`
}

type unfollowRequest struct {
	AccountID uuid.UUID `json:"-" path:"account_id"`
}

// Follow serves POST /v1/follows.
func (h *Handler) Follow(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in followRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Follow(r.Context(), p.AccountID, in.AccountID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unfollow serves DELETE /v1/follows/{account_id}.
func (h *Handler) Unfollow(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in unfollowRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.Unfollow(r.Context(), p.AccountID, in.AccountID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Follows serves GET /v1/follows.
func (h *Handler) Follows(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in page.Request
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.Follows(r.Context(), p.AccountID, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
)

type postRequest struct {
	listingBody

	IdempotencyKey string `header:"Idempotency-Key" json:"-"`
}

// Post serves POST /v1/listings.
func (h *Handler) Post(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in postRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Post(r.Context(), p.AccountID, in.IdempotencyKey, in.input())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, v)
}

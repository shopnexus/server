package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type updateRequest struct {
	draftBody

	ID             uuid.UUID `json:"-"                 path:"id"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
}

// Update serves PUT /v1/listings/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in updateRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Edit(r.Context(), p.AccountID, in.ID, in.IdempotencyKey, in.draft())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

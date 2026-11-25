package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type photoCopyRequest struct {
	ID  uuid.UUID `json:"-"                 path:"id"`
	Key uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// CopyPhotos serves POST /v1/listings/{id}/photo-copies.
func (h *Handler) CopyPhotos(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in photoCopyRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	copies, err := h.svc.CopyPhotos(r.Context(), p.AccountID, in.ID, in.Key)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, list[service.DraftPhotoView]{Items: copies})
}

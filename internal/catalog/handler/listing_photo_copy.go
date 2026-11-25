package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type photoCopyRequest struct {
	ID  uuid.UUID `json:"-"                 path:"id"`
	Key uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// CopyPhotos serves POST /v1/listings/{id}/photo-copies.
func (h *Handler) CopyPhotos(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusCreated,
		func(p authn.Principal, in photoCopyRequest) (list[service.DraftPhotoView], error) {
			copies, err := h.svc.CopyPhotos(r.Context(), p.AccountID, in.ID, in.Key)
			return list[service.DraftPhotoView]{Items: copies}, err
		})
}

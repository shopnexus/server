package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// startReadRequest is ListingReadRequest; the request validator lets
// exactly one source through.
type startReadRequest struct {
	Video      *uuid.UUID `json:"video"`
	Photo      *uuid.UUID `json:"photo"`
	Transcript string     `json:"transcript"`
	Key        uuid.UUID  `header:"Idempotency-Key" json:"-"`
}

// StartRead serves POST /v1/listing-reads.
func (h *Handler) StartRead(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusAccepted, func(p authn.Principal, in startReadRequest) (service.ListingReadView, error) {
		switch {
		case in.Video != nil:
			return h.svc.StartVideoRead(r.Context(), p.AccountID, in.Key, *in.Video, in.Transcript)
		case in.Photo != nil:
			return h.svc.StartPhotoRead(r.Context(), p.AccountID, in.Key, *in.Photo)
		default:
			return service.ListingReadView{}, domain.ErrNoSource
		}
	})
}

type readRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetRead serves GET /v1/listing-reads/{id}.
func (h *Handler) GetRead(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in readRequest) (service.ListingReadView, error) {
		return h.svc.GetRead(r.Context(), p.AccountID, in.ID)
	})
}

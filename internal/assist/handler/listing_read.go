package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/assist/domain"
	"github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// startReadRequest is ListingReadRequest; the request validator lets
// exactly one source through.
type startReadRequest struct {
	Video      *uuid.UUID `json:"video"`
	Photo      *uuid.UUID `json:"photo"`
	Link       *string    `json:"link"`
	Transcript string     `json:"transcript"`
	Key        uuid.UUID  `header:"Idempotency-Key" json:"-"`
}

// StartRead serves POST /v1/listing-reads.
func (h *Handler) StartRead(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in startReadRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	var (
		v   service.ListingReadView
		err error
	)
	switch {
	case in.Video != nil:
		v, err = h.svc.StartVideoRead(r.Context(), p.AccountID, in.Key, *in.Video, in.Transcript)
	case in.Photo != nil:
		v, err = h.svc.StartPhotoRead(r.Context(), p.AccountID, in.Key, *in.Photo)
	case in.Link != nil:
		v, err = h.svc.StartLinkRead(r.Context(), p.AccountID, in.Key, *in.Link)
	default:
		err = domain.ErrNoSource
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusAccepted, v)
}

type readRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetRead serves GET /v1/listing-reads/{id}.
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

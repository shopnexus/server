package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// errPhotoTooLarge answers a photo over domain.MaxPhotoBytes: the schema's
// maximum is the video's, so the handler holds the photo's.
var errPhotoTooLarge = apperr.New(apperr.KindBadRequest, "invalid_request", "a photo is at most 15 MB").
	WithField("size", "maximum")

type createUploadRequest struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// CreateUpload serves POST /v1/uploads.
func (h *Handler) CreateUpload(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in createUploadRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if !domain.IsVideo(in.ContentType) && in.Size > domain.MaxPhotoBytes {
		httpx.WriteError(w, r, h.log, errPhotoTooLarge)
		return
	}
	ticket, err := h.svc.CreateUpload(r.Context(), p.AccountID, in.ContentType, in.Size)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, ticket)
}

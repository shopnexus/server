package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/apperr"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// errPhotoTooLarge answers a photo over domain.MaxPhotoBytes: the schema's
// maximum is the video's, so the handler holds the photo's.
var errPhotoTooLarge = apperr.New(apperr.KindBadRequest, "invalid_request", "a photo is at most 15 MB").
	WithField("size", apperr.OutOfRange, "maximum")

type createUploadRequest struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// CreateUpload serves POST /v1/uploads.
func (h *Handler) CreateUpload(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusCreated, func(p authn.Principal, in createUploadRequest) (service.UploadTicket, error) {
		if !domain.IsVideo(in.ContentType) && in.Size > domain.MaxPhotoBytes {
			return service.UploadTicket{}, errPhotoTooLarge
		}
		return h.svc.CreateUpload(r.Context(), p.AccountID, in.ContentType, in.Size)
	})
}

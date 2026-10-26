package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

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
	ticket, err := h.svc.CreateUpload(r.Context(), p.AccountID, in.ContentType, in.Size)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusCreated, ticket)
}

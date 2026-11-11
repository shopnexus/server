package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/platform/httpx"
)

type linkRequest struct {
	URL string `json:"url"`
}

// PreviewLink serves POST /v1/link-previews.
func (h *Handler) PreviewLink(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in linkRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.PreviewLink(r.Context(), p.AccountID, in.URL)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

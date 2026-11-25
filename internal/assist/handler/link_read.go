package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type linkRequest struct {
	URL string `json:"url"`
}

// PreviewLink serves POST /v1/link-previews.
func (h *Handler) PreviewLink(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in linkRequest) (service.LinkPreviewView, error) {
		return h.svc.PreviewLink(r.Context(), p.AccountID, in.URL)
	})
}

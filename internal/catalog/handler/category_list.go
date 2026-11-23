package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

// Categories serves GET /v1/categories, named in the request's language.
func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.svc.Categories(r.Context(), locale.Negotiate(w, r))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, list[domain.Category]{Items: cats})
}

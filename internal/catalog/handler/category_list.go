package handler

import (
	"net/http"

	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

// Categories serves GET /v1/categories, named in the request's language.
func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(httpx.None) (list[domain.Category], error) {
		cats, err := h.svc.Categories(r.Context(), locale.Negotiate(w, r))
		return list[domain.Category]{Items: cats}, err
	})
}

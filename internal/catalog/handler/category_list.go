package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// Categories serves GET /v1/categories.
func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.svc.Categories(r.Context())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, list[domain.Category]{Items: cats})
}

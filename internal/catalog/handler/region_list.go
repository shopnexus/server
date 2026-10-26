package handler

import (
	"net/http"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// Provinces serves GET /v1/regions.
func (h *Handler) Provinces(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.Provinces(r.Context())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, list[domain.Province]{Items: ps})
}

type communesRequest struct {
	ProvinceCode string `json:"-" path:"province_code"`
}

// Communes serves GET /v1/regions/{province_code}/communes.
func (h *Handler) Communes(w http.ResponseWriter, r *http.Request) {
	var in communesRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	cs, err := h.svc.Communes(r.Context(), in.ProvinceCode)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, list[domain.Commune]{Items: cs})
}

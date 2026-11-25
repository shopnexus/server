package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/console/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type accountRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// GetAccount serves GET /v1/console/accounts/{id}.
func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	httpx.Serve(w, r, h.log, http.StatusOK, func(in accountRequest) (service.AccountDetail, error) {
		return h.svc.GetAccount(r.Context(), in.ID)
	})
}

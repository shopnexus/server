package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

// History serves GET /v1/search/history.
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, _ httpx.None) (service.HistoryView, error) {
		return h.svc.History(r.Context(), p.AccountID)
	})
}

// ClearHistory serves DELETE /v1/search/history.
func (h *Handler) ClearHistory(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, _ httpx.None) (httpx.None, error) {
		return httpx.None{}, h.svc.ClearHistory(r.Context(), p.AccountID)
	})
}

type historyItemRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// ForgetSearch serves DELETE /v1/search/history/{id}.
func (h *Handler) ForgetSearch(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusNoContent, func(p authn.Principal, in historyItemRequest) (httpx.None, error) {
		return httpx.None{}, h.svc.ForgetSearch(r.Context(), p.AccountID, in.ID)
	})
}

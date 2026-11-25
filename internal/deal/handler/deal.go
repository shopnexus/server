package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/deal/domain"
	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
)

type openRequest struct {
	ListingID uuid.UUID `json:"listing_id"`
}

type dealRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

type offerRequest struct {
	ID     uuid.UUID `json:"-"      path:"id"`
	Amount int64     `json:"amount"`
}

type acceptRequest struct {
	ID    uuid.UUID `json:"-"     path:"id"`
	Price int64     `json:"price"`
}

type listMineRequest struct {
	page.Request

	Role domain.ListRole `json:"-" query:"role"`
}

// Open serves POST /v1/deals: 201 with a new deal, 200 with the caller's
// existing one.
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in openRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, created, err := h.svc.Open(r.Context(), p.AccountID, in.ListingID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.Respond(w, status, v)
}

// ListMine serves GET /v1/me/deals.
func (h *Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in listMineRequest) (page.Page[service.DealView], error) {
		return h.svc.ListMine(r.Context(), p.AccountID, in.Role.Party(), in.Request)
	})
}

// Get serves GET /v1/deals/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in dealRequest) (service.DealView, error) {
		return h.svc.Get(r.Context(), p.AccountID, in.ID)
	})
}

// Offer serves POST /v1/deals/{id}/offer.
func (h *Handler) Offer(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in offerRequest) (service.DealView, error) {
		return h.svc.Offer(r.Context(), p.AccountID, in.ID, in.Amount)
	})
}

// Accept serves POST /v1/deals/{id}/accept.
func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in acceptRequest) (service.DealView, error) {
		return h.svc.Accept(r.Context(), p.AccountID, in.ID, in.Price)
	})
}

// Decline serves POST /v1/deals/{id}/decline.
func (h *Handler) Decline(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in dealRequest) (service.DealView, error) {
		return h.svc.Decline(r.Context(), p.AccountID, in.ID)
	})
}

// Withdraw serves POST /v1/deals/{id}/withdraw.
func (h *Handler) Withdraw(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in dealRequest) (service.DealView, error) {
		return h.svc.Withdraw(r.Context(), p.AccountID, in.ID)
	})
}

// Cancel serves POST /v1/deals/{id}/cancel.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in dealRequest) (service.DealView, error) {
		return h.svc.Cancel(r.Context(), p.AccountID, in.ID)
	})
}

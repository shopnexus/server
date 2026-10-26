package handler

import (
	"context"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
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

	Role string `json:"-" query:"role"`
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
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in listMineRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	as := domain.Buyer
	if in.Role == "selling" {
		as = domain.Seller
	}
	out, err := h.svc.ListMine(r.Context(), p.AccountID, as, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

// Get serves GET /v1/deals/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in dealRequest) (service.DealView, error) {
		return h.svc.Get(ctx, caller, in.ID)
	})
}

// Offer serves POST /v1/deals/{id}/offer.
func (h *Handler) Offer(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in offerRequest) (service.DealView, error) {
		return h.svc.Offer(ctx, caller, in.ID, in.Amount)
	})
}

// Accept serves POST /v1/deals/{id}/accept.
func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in acceptRequest) (service.DealView, error) {
		return h.svc.Accept(ctx, caller, in.ID, in.Price)
	})
}

// Decline serves POST /v1/deals/{id}/decline.
func (h *Handler) Decline(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in dealRequest) (service.DealView, error) {
		return h.svc.Decline(ctx, caller, in.ID)
	})
}

// Withdraw serves POST /v1/deals/{id}/withdraw.
func (h *Handler) Withdraw(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in dealRequest) (service.DealView, error) {
		return h.svc.Withdraw(ctx, caller, in.ID)
	})
}

// Cancel serves POST /v1/deals/{id}/cancel.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in dealRequest) (service.DealView, error) {
		return h.svc.Cancel(ctx, caller, in.ID)
	})
}

// serve binds the request of a call on one deal, applies it as the caller
// and answers 200 with the deal.
func serve[T any](h *Handler, w http.ResponseWriter, r *http.Request,
	apply func(ctx context.Context, caller uuid.UUID, in T) (service.DealView, error),
) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in T
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := apply(r.Context(), p.AccountID, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

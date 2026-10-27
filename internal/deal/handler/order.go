package handler

import (
	"context"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type codRequest struct {
	ID        uuid.UUID `json:"-"          path:"id"`
	AddressID uuid.UUID `json:"address_id"`
}

type shipRequest struct {
	ID             uuid.UUID `json:"-"                 path:"id"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
	Carrier        string    `json:"carrier"`
	CarrierName    string    `json:"carrier_name"`
	TrackingCode   string    `json:"tracking_code"`
	Fee            int64     `json:"fee"`
}

type finishRequest struct {
	ID             uuid.UUID `json:"-"                 path:"id"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
}

// RequestCod serves POST /v1/deals/{id}/cod.
func (h *Handler) RequestCod(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in codRequest) (service.DealView, error) {
		return h.svc.RequestCod(ctx, caller, in.ID, in.AddressID)
	})
}

// Ship serves POST /v1/deals/{id}/ship.
func (h *Handler) Ship(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in shipRequest) (service.DealView, error) {
		return h.svc.Ship(ctx, caller, in.ID, in.IdempotencyKey, service.WaybillInput{
			Carrier: in.Carrier, CarrierName: in.CarrierName, TrackingCode: in.TrackingCode, Fee: in.Fee,
		})
	})
}

// Deliver serves POST /v1/deals/{id}/delivered.
func (h *Handler) Deliver(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in finishRequest) (service.DealView, error) {
		return h.svc.Deliver(ctx, caller, in.ID, in.IdempotencyKey)
	})
}

// Return serves POST /v1/deals/{id}/returned.
func (h *Handler) Return(w http.ResponseWriter, r *http.Request) {
	serve(h, w, r, func(ctx context.Context, caller uuid.UUID, in finishRequest) (service.DealView, error) {
		return h.svc.Return(ctx, caller, in.ID, in.IdempotencyKey)
	})
}

// ListOrders serves GET /v1/me/orders.
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
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
	out, err := h.svc.ListOrders(r.Context(), p.AccountID, as, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

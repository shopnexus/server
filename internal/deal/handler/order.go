package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type codRequest struct {
	ID             uuid.UUID `json:"-"                 path:"id"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
	Price          int64     `json:"price"`
	AddressID      uuid.UUID `json:"address_id"`
}

// RequestCod serves POST /v1/deals/{id}/cod.
func (h *Handler) RequestCod(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in codRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.RequestCod(r.Context(), p.AccountID, in.ID, in.Price, in.AddressID, in.IdempotencyKey)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// shipmentRequest is ShipmentDraft.
type shipmentRequest struct {
	OrderID        uuid.UUID `json:"-"                 path:"order_id"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
	Carrier        string    `json:"carrier"`
	Service        string    `json:"service"`
	WeightGrams    int       `json:"weight_grams"`
	Pickup         string    `json:"pickup"`
	PickupFrom     *struct {
		SavedPickup     bool      `json:"saved_pickup"`
		CarrierPickupID string    `json:"carrier_pickup_id"`
		AddressID       uuid.UUID `json:"address_id"`
	} `json:"pickup_from"`
}

type confirmRequest struct {
	OrderID        uuid.UUID `json:"-"                 path:"order_id"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
}

// CreateShipment serves POST /v1/orders/{order_id}/shipment.
func (h *Handler) CreateShipment(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in shipmentRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	ship := service.ShipmentInput{Carrier: in.Carrier, Service: in.Service, WeightGrams: in.WeightGrams, Pickup: in.Pickup}
	if f := in.PickupFrom; f != nil {
		ship.PickupPointID, ship.PickupAddressID, ship.SavedPickup = f.CarrierPickupID, f.AddressID, f.SavedPickup
	}
	v, err := h.svc.CreateShipment(r.Context(), p.AccountID, in.OrderID, in.IdempotencyKey, ship)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// ConfirmReceipt serves POST /v1/orders/{order_id}/confirm.
func (h *Handler) ConfirmReceipt(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in confirmRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.ConfirmReceipt(r.Context(), p.AccountID, in.OrderID, in.IdempotencyKey)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

package handler

import (
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/deal/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type codRequest struct {
	ID        uuid.UUID `json:"-"                 path:"id"`
	Key       uuid.UUID `header:"Idempotency-Key" json:"-"`
	Price     int64     `json:"price"`
	AddressID uuid.UUID `json:"address_id"`
}

// RequestCod serves POST /v1/deals/{id}/cod.
func (h *Handler) RequestCod(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in codRequest) (service.DealView, error) {
		return h.svc.RequestCod(r.Context(), p.AccountID, in.ID, in.Price, in.AddressID, in.Key)
	})
}

// shipmentRequest is ShipmentDraft.
type shipmentRequest struct {
	OrderID     uuid.UUID `json:"-"                 path:"order_id"`
	Key         uuid.UUID `header:"Idempotency-Key" json:"-"`
	Carrier     string    `json:"carrier"`
	Service     string    `json:"service"`
	WeightGrams int       `json:"weight_grams"`
	Pickup      string    `json:"pickup"`
	PickupFrom  *struct {
		SavedPickup     bool      `json:"saved_pickup"`
		CarrierPickupID string    `json:"carrier_pickup_id"`
		AddressID       uuid.UUID `json:"address_id"`
	} `json:"pickup_from"`
}

type confirmRequest struct {
	OrderID uuid.UUID `json:"-"                 path:"order_id"`
	Key     uuid.UUID `header:"Idempotency-Key" json:"-"`
}

// CreateShipment serves POST /v1/orders/{order_id}/shipment.
func (h *Handler) CreateShipment(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in shipmentRequest) (service.DealView, error) {
		ship := service.ShipmentInput{Carrier: in.Carrier, Service: in.Service, WeightGrams: in.WeightGrams, Pickup: in.Pickup}
		if f := in.PickupFrom; f != nil {
			ship.PickupPointID, ship.PickupAddressID, ship.SavedPickup = f.CarrierPickupID, f.AddressID, f.SavedPickup
		}
		return h.svc.CreateShipment(r.Context(), p.AccountID, in.OrderID, in.Key, ship)
	})
}

// ConfirmReceipt serves POST /v1/orders/{order_id}/confirm.
func (h *Handler) ConfirmReceipt(w http.ResponseWriter, r *http.Request) {
	httpx.ServeCaller(w, r, h.log, http.StatusOK, func(p authn.Principal, in confirmRequest) (service.DealView, error) {
		return h.svc.ConfirmReceipt(r.Context(), p.AccountID, in.OrderID, in.Key)
	})
}

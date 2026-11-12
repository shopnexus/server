package handler

import (
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
	v, err := h.svc.RequestCod(r.Context(), p.AccountID, in.ID, in.AddressID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// shipmentRequest is ShipmentDraft: the request validator lets through
// the fields of one mode only.
type shipmentRequest struct {
	OrderID        uuid.UUID `json:"-"                 path:"order_id"`
	IdempotencyKey string    `header:"Idempotency-Key" json:"-"`
	Mode           string    `json:"mode"`
	Carrier        string    `json:"carrier"`
	TrackingCode   string    `json:"tracking_code"`
	Service        string    `json:"service"`
	WeightGrams    int       `json:"weight_grams"`
	Pickup         string    `json:"pickup"`
}

type trackingRequest struct {
	OrderID      uuid.UUID `json:"-"             path:"order_id"`
	Carrier      string    `json:"carrier"`
	TrackingCode string    `json:"tracking_code"`
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
	v, err := h.svc.CreateShipment(r.Context(), p.AccountID, in.OrderID, in.IdempotencyKey, service.ShipmentInput{
		Mode: in.Mode, Carrier: in.Carrier, TrackingCode: in.TrackingCode,
		Service: in.Service, WeightGrams: in.WeightGrams, Pickup: in.Pickup,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// EditShipment serves PUT /v1/orders/{order_id}/shipment.
func (h *Handler) EditShipment(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in trackingRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.EditShipment(r.Context(), p.AccountID, in.OrderID, in.Carrier, in.TrackingCode)
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

// Package handler serves module deal's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the deal endpoints, all behind protect. With the fake
// carrier it also adds the dev endpoint that moves the fake's shipments,
// open to anyone.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler, fakeCarrier bool) {
	r.Handle("POST /v1/deals", protect(http.HandlerFunc(h.Open)))
	r.Handle("GET /v1/deals/{id}", protect(http.HandlerFunc(h.Get)))
	r.Handle("POST /v1/deals/{id}/offer", protect(http.HandlerFunc(h.Offer)))
	r.Handle("POST /v1/deals/{id}/accept", protect(http.HandlerFunc(h.Accept)))
	r.Handle("POST /v1/deals/{id}/decline", protect(http.HandlerFunc(h.Decline)))
	r.Handle("POST /v1/deals/{id}/withdraw", protect(http.HandlerFunc(h.Withdraw)))
	r.Handle("POST /v1/deals/{id}/cancel", protect(http.HandlerFunc(h.Cancel)))
	r.Handle("GET /v1/me/deals", protect(http.HandlerFunc(h.ListMine)))
	r.Handle("POST /v1/deals/{id}/cod", protect(http.HandlerFunc(h.RequestCod)))
	r.Handle("POST /v1/orders/{order_id}/shipment", protect(http.HandlerFunc(h.CreateShipment)))
	r.Handle("PUT /v1/orders/{order_id}/shipment", protect(http.HandlerFunc(h.EditShipment)))
	r.Handle("POST /v1/orders/{order_id}/cancel", protect(http.HandlerFunc(h.CancelOrder)))
	r.Handle("POST /v1/orders/{order_id}/confirm", protect(http.HandlerFunc(h.ConfirmReceipt)))
	r.Handle("GET /v1/me/orders", protect(http.HandlerFunc(h.ListOrders)))
	r.Handle("GET /v1/shipments/lookup", protect(http.HandlerFunc(h.LookupShipment)))
	if fakeCarrier {
		r.Handle("POST /v1/dev/shipments/{code}/status", http.HandlerFunc(h.SetShipmentStatus))
	}
}

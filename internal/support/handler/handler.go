// Package handler serves module support's HTTP API for people. Console
// members reach tickets through console's handler.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/support/service"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the /v1/support endpoints behind protect.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("GET /v1/support/tickets", protect(http.HandlerFunc(h.List)))
	r.Handle("POST /v1/support/tickets", protect(http.HandlerFunc(h.Open)))
	r.Handle("GET /v1/support/summary", protect(http.HandlerFunc(h.Summary)))
	r.Handle("GET /v1/support/orders/{order_id}/ticket", protect(http.HandlerFunc(h.OfOrder)))
}

type openRequest struct {
	Key         uuid.UUID  `header:"Idempotency-Key" json:"-"`
	Kind        string     `json:"kind"`
	OrderID     *uuid.UUID `json:"order_id"`
	CarrierName string     `json:"carrier_name"`
	Body        string     `json:"body"`
}

// Open serves POST /v1/support/tickets: 201 for a new ticket, 200 for the
// one the key or the order has.
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
	out, existed, err := h.svc.Open(r.Context(), p.AccountID, in.Key, service.OpenInput{
		Kind: in.Kind, OrderID: in.OrderID, CarrierName: in.CarrierName, Body: in.Body,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	status := http.StatusCreated
	if existed {
		status = http.StatusOK
	}
	httpx.Respond(w, status, out)
}

// List serves GET /v1/support/tickets.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	items, err := h.svc.Mine(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, map[string]any{"items": items})
}

// Summary serves GET /v1/support/summary.
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	out, err := h.svc.Summary(r.Context(), p.AccountID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

type orderRequest struct {
	OrderID uuid.UUID `json:"-" path:"order_id"`
}

// OfOrder serves GET /v1/support/orders/{order_id}/ticket.
func (h *Handler) OfOrder(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in orderRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, err := h.svc.OfOrder(r.Context(), p.AccountID, in.OrderID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, out)
}

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

// Register adds the deal endpoints, all behind protect.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("POST /v1/deals", protect(http.HandlerFunc(h.Open)))
	r.Handle("GET /v1/deals/{id}", protect(http.HandlerFunc(h.Get)))
	r.Handle("POST /v1/deals/{id}/offer", protect(http.HandlerFunc(h.Offer)))
	r.Handle("POST /v1/deals/{id}/accept", protect(http.HandlerFunc(h.Accept)))
	r.Handle("POST /v1/deals/{id}/decline", protect(http.HandlerFunc(h.Decline)))
	r.Handle("POST /v1/deals/{id}/withdraw", protect(http.HandlerFunc(h.Withdraw)))
	r.Handle("POST /v1/deals/{id}/cancel", protect(http.HandlerFunc(h.Cancel)))
	r.Handle("GET /v1/me/deals", protect(http.HandlerFunc(h.ListMine)))
	r.Handle("POST /v1/deals/{id}/cod", protect(http.HandlerFunc(h.RequestCod)))
	r.Handle("POST /v1/deals/{id}/ship", protect(http.HandlerFunc(h.Ship)))
	r.Handle("POST /v1/deals/{id}/delivered", protect(http.HandlerFunc(h.Deliver)))
	r.Handle("POST /v1/deals/{id}/returned", protect(http.HandlerFunc(h.Return)))
	r.Handle("GET /v1/me/orders", protect(http.HandlerFunc(h.ListOrders)))
}

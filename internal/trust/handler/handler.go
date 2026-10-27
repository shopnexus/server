// Package handler serves module trust's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/service"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the trust endpoints; a seller's reviews are public.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("PUT /v1/orders/{order_id}/review", protect(http.HandlerFunc(h.PutReview)))
	r.Handle("DELETE /v1/orders/{order_id}/review", protect(http.HandlerFunc(h.DeleteReview)))
	r.Handle("GET /v1/sellers/{id}/reviews", http.HandlerFunc(h.SellerReviews))
}

type putRequest struct {
	OrderID uuid.UUID `json:"-"     path:"order_id"`
	Stars   int       `json:"stars"`
	Text    string    `json:"text"`
}

type orderRequest struct {
	OrderID uuid.UUID `json:"-" path:"order_id"`
}

type sellerRequest struct {
	page.Request

	ID uuid.UUID `json:"-" path:"id"`
}

// PutReview serves PUT /v1/orders/{order_id}/review.
func (h *Handler) PutReview(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in putRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.PutReview(r.Context(), p.AccountID, in.OrderID, in.Stars, in.Text)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// DeleteReview serves DELETE /v1/orders/{order_id}/review.
func (h *Handler) DeleteReview(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in orderRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.DeleteReview(r.Context(), p.AccountID, in.OrderID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SellerReviews serves GET /v1/sellers/{id}/reviews to anyone.
func (h *Handler) SellerReviews(w http.ResponseWriter, r *http.Request) {
	var in sellerRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.SellerReviews(r.Context(), in.ID, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

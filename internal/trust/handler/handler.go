// Package handler serves module trust's HTTP API.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/service"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the trust endpoints; a person's reviews are public, with
// the reader's helpful marks when identify finds one, and writing reviews,
// marks, reports and scam warnings need a signed-in caller.
func (h *Handler) Register(r *httpx.Router, protect, identify func(http.Handler) http.Handler) {
	r.Handle("PUT /v1/orders/{order_id}/review", protect(http.HandlerFunc(h.PutReview)))
	r.Handle("DELETE /v1/orders/{order_id}/review", protect(http.HandlerFunc(h.DeleteReview)))
	r.Handle("POST /v1/reviews/{id}/reply", protect(http.HandlerFunc(h.ReplyReview)))
	r.Handle("PUT /v1/reviews/{id}/helpful", protect(http.HandlerFunc(h.MarkHelpful)))
	r.Handle("DELETE /v1/reviews/{id}/helpful", protect(http.HandlerFunc(h.UnmarkHelpful)))
	r.Handle("GET /v1/accounts/{id}/reviews", identify(http.HandlerFunc(h.Reviews)))
	r.Handle("GET /v1/sellers/{id}/reviews", identify(http.HandlerFunc(h.Reviews)))
	r.Handle("GET /v1/reports", protect(http.HandlerFunc(h.MyReports)))
	r.Handle("POST /v1/reports", protect(http.HandlerFunc(h.Report)))
	r.Handle("DELETE /v1/reports/{id}", protect(http.HandlerFunc(h.WithdrawReport)))
	r.Handle("POST /v1/conversations/{id}/scam-warning/dismiss", protect(http.HandlerFunc(h.DismissScamWarning)))
}

type putRequest struct {
	OrderID uuid.UUID `json:"-"     path:"order_id"`
	Stars   int       `json:"stars"`
	Text    string    `json:"text"`
	// Photos is nil when the body leaves them out, which keeps them.
	Photos []uuid.UUID `json:"photos"`
}

type orderRequest struct {
	OrderID uuid.UUID `json:"-" path:"order_id"`
}

type reviewsRequest struct {
	page.Request

	ID   uuid.UUID   `json:"-" path:"id"`
	Role domain.Role `json:"-" query:"role"`
}

type reviewRequest struct {
	ID uuid.UUID `json:"-" path:"id"`
}

type replyRequest struct {
	ID   uuid.UUID `json:"-"    path:"id"`
	Text string    `json:"text"`
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
	v, err := h.svc.PutReview(r.Context(), p.AccountID, in.OrderID, in.Stars, in.Text, in.Photos)
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

// ReplyReview serves POST /v1/reviews/{id}/reply.
func (h *Handler) ReplyReview(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in replyRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.ReplyReview(r.Context(), p.AccountID, in.ID, in.Text)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

// MarkHelpful serves PUT /v1/reviews/{id}/helpful.
func (h *Handler) MarkHelpful(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in reviewRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.MarkHelpful(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UnmarkHelpful serves DELETE /v1/reviews/{id}/helpful.
func (h *Handler) UnmarkHelpful(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in reviewRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if err := h.svc.UnmarkHelpful(r.Context(), p.AccountID, in.ID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Reviews serves GET /v1/accounts/{id}/reviews to anyone, and
// GET /v1/sellers/{id}/reviews, which has no role and reads the seller's.
func (h *Handler) Reviews(w http.ResponseWriter, r *http.Request) {
	var in reviewsRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if in.Role == "" {
		in.Role = domain.RoleSeller
	}
	v, err := h.svc.Reviews(r.Context(), httpx.Viewer(r), in.ID, in.Role, in.Request)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	httpx.Respond(w, http.StatusOK, v)
}

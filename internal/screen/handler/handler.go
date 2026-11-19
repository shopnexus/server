// Package handler serves module screen's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/screen/service"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the screen endpoints. protect requires a signed-in caller;
// identify reads one when the request carries it.
func (h *Handler) Register(r *httpx.Router, protect, identify func(http.Handler) http.Handler) {
	r.Handle("GET /v1/screens/order/{order_id}", protect(http.HandlerFunc(h.Order)))
	r.Handle("GET /v1/screens/orders", protect(http.HandlerFunc(h.Orders)))
	r.Handle("GET /v1/screens/my-listings", protect(http.HandlerFunc(h.MyListings)))
	r.Handle("GET /v1/screens/cod-setup", protect(http.HandlerFunc(h.CodSetup)))
	r.Handle("GET /v1/screens/cod-checkout/{deal_id}", protect(http.HandlerFunc(h.CodCheckout)))
	r.Handle("GET /v1/screens/home", identify(http.HandlerFunc(h.Home)))
	r.Handle("GET /v1/screens/listing/{id}", identify(http.HandlerFunc(h.Listing)))
	r.Handle("GET /v1/screens/seller/{id}", identify(http.HandlerFunc(h.Seller)))
	r.Handle("GET /v1/screens/inbox", protect(http.HandlerFunc(h.Inbox)))
	r.Handle("GET /v1/screens/inbox/search", protect(http.HandlerFunc(h.InboxSearch)))
	r.Handle("GET /v1/screens/chat/{conversation_id}", protect(http.HandlerFunc(h.Chat)))
	r.Handle("GET /v1/screens/profile-tasks", protect(http.HandlerFunc(h.ProfileTasks)))
	r.Handle("GET /v1/screens/reputation", protect(http.HandlerFunc(h.Reputation)))
	r.Handle("GET /v1/me/unread", protect(http.HandlerFunc(h.Unread)))
}

// Package handler serves module beacon's HTTP API.
package handler

import (
	"log/slog"
	"net/http"

	"github.com/shopnexus/server/internal/beacon/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds POST /v1/client-events, open to anyone.
func (h *Handler) Register(r *httpx.Router) {
	r.HandleFunc("POST /v1/client-events", h.SendClientEvents)
}

// SendClientEvents serves POST /v1/client-events.
func (h *Handler) SendClientEvents(w http.ResponseWriter, r *http.Request) {
	var in service.Batch
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	h.svc.Record(r.Context(), in)
	w.WriteHeader(http.StatusAccepted)
}

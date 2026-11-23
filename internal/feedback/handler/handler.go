// Package handler serves module feedback's HTTP API for people. Staff reach
// feedback through console's handler.
package handler

import (
	"log/slog"
	"net/http"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
	"github.com/shopnexus/server/internal/feedback/service"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the /v1/feedback endpoints behind protect.
func (h *Handler) Register(r *httpx.Router, protect func(http.Handler) http.Handler) {
	r.Handle("GET /v1/feedback", protect(http.HandlerFunc(h.Mine)))
	r.Handle("POST /v1/feedback", protect(http.HandlerFunc(h.Send)))
}

type sendRequest struct {
	Key     uuid.UUID      `header:"Idempotency-Key" json:"-"`
	Mood    int            `json:"mood"`
	Kind    string         `json:"kind"`
	Text    string         `json:"text"`
	Photos  []uuid.UUID    `json:"photos"`
	Device  *domain.Device `json:"device"`
	Screen  string         `json:"screen"`
	Contact bool           `json:"contact"`
}

// Send serves POST /v1/feedback: 201 for new feedback, 200 for the one the
// key made.
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	p, ok := httpx.Caller(w, r, h.log)
	if !ok {
		return
	}
	var in sendRequest
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	out, existed, err := h.svc.Send(r.Context(), p.AccountID, in.Key, service.SendInput{
		Mood: in.Mood, Kind: in.Kind, Text: in.Text, Photos: in.Photos, Device: in.Device, Screen: in.Screen, Contact: in.Contact,
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

// Mine serves GET /v1/feedback.
func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
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

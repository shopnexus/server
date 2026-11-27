// Package handler serves module monitoring's console endpoints, for admins
// and their console agent.
package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/shopnexus/server/internal/monitoring/domain"
	"github.com/shopnexus/server/internal/monitoring/service"
	"github.com/shopnexus/server/internal/platform/authn"
	"github.com/shopnexus/server/internal/platform/httpx"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the /v1/console/monitoring endpoints behind admin.
func (h *Handler) Register(r *httpx.Router, admin func(http.Handler) http.Handler) {
	r.Handle("GET /v1/console/monitoring/health", admin(http.HandlerFunc(h.Health)))
	r.Handle("GET /v1/console/monitoring/metrics", admin(http.HandlerFunc(h.ListMetrics)))
	r.Handle("GET /v1/console/monitoring/metrics/query", admin(http.HandlerFunc(h.QueryMetrics)))
	r.Handle("GET /v1/console/monitoring/logs", admin(http.HandlerFunc(h.QueryLogs)))
}

// defaultWindow is the spec's default window.
const defaultWindow = 15 * time.Minute

type healthRequest struct {
	Window *domain.Duration `json:"-" query:"window"`
}

// Health serves GET /v1/console/monitoring/health.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, in healthRequest) (service.Health, error) {
		window := defaultWindow
		if in.Window != nil {
			window = time.Duration(*in.Window)
		}
		return h.svc.Health(r.Context(), window)
	})
}

type listRequest struct {
	Match string `json:"-" query:"match"`
}

// ListMetrics serves GET /v1/console/monitoring/metrics.
func (h *Handler) ListMetrics(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, in listRequest) (service.MetricList, error) {
		return h.svc.ListMetrics(r.Context(), in.Match)
	})
}

type queryRequest struct {
	Query string          `json:"-" query:"query"`
	Range domain.Duration `json:"-" query:"range"`
	End   *time.Time      `json:"-" query:"end"`
}

func (q queryRequest) query() service.Query {
	out := service.Query{Text: q.Query, Range: time.Duration(q.Range)}
	if q.End != nil {
		out.End = *q.End
	}
	return out
}

type metricsRequest struct {
	queryRequest

	Step domain.Duration `json:"-" query:"step"`
}

// QueryMetrics serves GET /v1/console/monitoring/metrics/query.
func (h *Handler) QueryMetrics(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, in metricsRequest) (service.MetricsResult, error) {
		return h.svc.QueryMetrics(r.Context(), in.query(), time.Duration(in.Step))
	})
}

// defaultLines is the spec's default limit of a log query.
const defaultLines = 100

type logsRequest struct {
	queryRequest

	Limit *int `json:"-" query:"limit"`
}

// QueryLogs serves GET /v1/console/monitoring/logs.
func (h *Handler) QueryLogs(w http.ResponseWriter, r *http.Request) {
	httpx.ServeConsole(w, r, h.log, http.StatusOK, func(_ authn.ConsoleMember, in logsRequest) (service.LogsResult, error) {
		limit := defaultLines
		if in.Limit != nil {
			limit = *in.Limit
		}
		return h.svc.QueryLogs(r.Context(), in.query(), limit)
	})
}

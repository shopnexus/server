// Package monitoring answers an admin's questions about the system's
// health: it reads the metrics from Prometheus and the logs from Loki, the
// ones the stack's lgtm holds, within the limits of ADR-0007. The console
// agent reaches it as any console API, with the admin's rights.
package monitoring

import (
	"net/http"

	"github.com/shopnexus/server/internal/monitoring/handler"
	"github.com/shopnexus/server/internal/monitoring/repository"
	"github.com/shopnexus/server/internal/monitoring/service"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/module"
)

// Mount registers the /v1/console/monitoring endpoints behind admin,
// console's guard of an admin's route.
func Mount(d module.Deps, cfg config.MonitoringConfig, admin func(http.Handler) http.Handler) {
	client := httpx.NewClient(0)
	svc := service.New(service.Deps{
		Metrics: repository.NewPrometheus(cfg.MetricsURL, client), Logs: repository.NewLoki(cfg.LogsURL, client), Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, admin)
}

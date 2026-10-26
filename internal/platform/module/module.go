// Package module holds what app hands every module's Mount. A module's Mount
// takes Deps, its own settings and the services of the modules it calls,
// builds its repositories, service and handler, registers its routes, and
// returns what app and other modules need from it: its service or Restate
// services.
package module

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/httpx"
)

// Deps is shared by every module.
type Deps struct {
	Router *httpx.Router
	Pool   *pgxpool.Pool
	// Ingress and ServicePrefix reach this process's Restate services.
	Ingress       *ingress.Client
	ServicePrefix string
	// Events is the bus a module subscribes its Restate handlers to in
	// Mount.
	Events *events.Bus
	Now    func() time.Time
	Log    *slog.Logger
}

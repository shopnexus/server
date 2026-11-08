// Package module holds what app hands every module's Mount. A module's Mount
// takes Deps, then its own Settings when it has any, then the services of the
// modules it calls. It builds its repositories, service and handler,
// registers its routes, and returns what app and other modules need from it:
// its Service, or a Mounted with its Restate services too.
package module

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
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
	// Protect requires a signed-in caller. Identify lets anyone through and
	// tells the handler who signed in; a caller who sends a token must send
	// a valid one. app builds both once from account's keys.
	Protect, Identify func(http.Handler) http.Handler
}

// Mounted is what a module with Restate services gives app: those services,
// with prefixed names, and the Service other modules call.
type Mounted[S any] struct {
	Services []restate.ServiceDefinition
	Service  S
}

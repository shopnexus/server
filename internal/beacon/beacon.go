// Package beacon records what the website and the app report about
// script errors, page speed, request times and search results, as metrics
// and log lines.
package beacon

import (
	"github.com/shopnexus/server/internal/beacon/handler"
	"github.com/shopnexus/server/internal/beacon/service"
	"github.com/shopnexus/server/internal/platform/module"
)

// Mount registers POST /v1/client-events.
func Mount(d module.Deps) {
	svc := service.New(service.Deps{Route: d.Router.Route, Log: d.Log})
	handler.New(svc, d.Log).Register(d.Router)
}

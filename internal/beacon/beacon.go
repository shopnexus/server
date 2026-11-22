// Package beacon records what the website and the app report about
// script errors, page speed, request times and search results, as metrics
// and log lines.
package beacon

import (
	"github.com/shopnexus/server/internal/beacon/handler"
	"github.com/shopnexus/server/internal/beacon/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/module"
)

// A page sends a batch every 15 s at most and the app every 30 s, so an
// address over this is a loop or abuse; carrier NAT puts many phones on one
// address, hence the room.
const (
	batchesPerSecond = 2
	batchBurst       = 60
)

// Mount registers POST /v1/client-events, rate limited per address.
func Mount(d module.Deps) {
	svc := service.New(service.Deps{Route: d.Router.Route, Now: d.Now, Log: d.Log})
	handler.New(svc, d.Log).Register(d.Router, httpx.RateLimit(batchesPerSecond, batchBurst, d.Now, d.Log))
}

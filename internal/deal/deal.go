// Package deal is a buyer and a seller agreeing a price on a listing.
package deal

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/handler"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/deal/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Mounted is what deal's Mount gives app and other modules.
type Mounted struct {
	Services []restate.ServiceDefinition
	Service  service.Service
}

// Mount registers the deal endpoints behind protect, subscribes the
// listing watch to the listing events, and returns deal's Restate services
// and the Service chat reads a pair's deals through.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, accounts accountsvc.Service,
	auth authsvc.Service, catalog catalogsvc.Service,
) Mounted {
	svc := service.New(service.Deps{
		Deals: repository.NewDeals(d.Pool), Accounts: accounts, Auth: auth, Catalog: catalog,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, protect)
	watch := worker.ListingWatch{Prefix: d.ServicePrefix, Deals: svc}
	for _, k := range []events.Kind{events.ListingUpdated, events.ListingHidden, events.ListingSold, events.ListingDeleted} {
		d.Events.Subscribe(k, watch.ServiceName(), "Changed")
	}
	return Mounted{Services: append(svc.Durable(), restate.Reflect(watch)), Service: svc}
}

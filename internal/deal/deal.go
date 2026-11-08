// Package deal is a buyer and a seller agreeing a price on a listing.
package deal

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/deal/handler"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/deal/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Mount registers the deal endpoints, subscribes the
// listing watch to the listing events, and returns deal's Restate services
// and the Service other modules read deals through.
func Mount(d module.Deps, accounts accountsvc.Service, catalog catalogsvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Deals: repository.NewDeals(d.Pool), Accounts: accounts, Catalog: catalog,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	watch := worker.ListingWatch{Prefix: d.ServicePrefix, Deals: svc}
	for _, k := range []events.Kind{events.ListingUpdated, events.ListingHidden, events.ListingSold, events.ListingDeleted} {
		d.Events.Subscribe(k, watch.ServiceName(), "Changed")
	}
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(watch)), Service: svc}
}

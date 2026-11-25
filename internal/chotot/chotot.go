// Package chotot links a seller's Chợ Tốt profile, with a code they put in
// its bio, and brings its listings over as listings: the ones the seller
// picks, and with auto_import the new ones, checked about once an hour.
// It follows the copies' prices and hides those whose listing left Chợ
// Tốt, and reminds the seller to hide on Chợ Tốt a listing whose copy sold
// here. ShopNexus only reads Chợ Tốt.
package chotot

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	assistsvc "github.com/shopnexus/server/internal/assist/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chotot/domain"
	"github.com/shopnexus/server/internal/chotot/handler"
	"github.com/shopnexus/server/internal/chotot/repository"
	"github.com/shopnexus/server/internal/chotot/service"
	"github.com/shopnexus/server/internal/chotot/worker"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
)

// Settings is what chotot's Mount takes beyond module.Deps.
type Settings struct {
	// Source reads Chợ Tốt.
	Source service.Source
	// CallTimeout bounds one call to Chợ Tốt.
	CallTimeout time.Duration
}

// Mount registers the chotot endpoints, subscribes the listing watch to
// listing.sold, listing.updated, listing.deleted and account.deleted, and
// returns chotot's Restate services and its Service.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, catalog catalogsvc.Service, media mediasvc.Service,
	assist assistsvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Codes: repository.NewCodes(d.Pool), Links: repository.NewLinks(d.Pool), Imports: repository.NewImports(d.Pool),
		Source: s.Source, Fill: assist, Accounts: accounts, Catalog: catalog, Media: media,
		Checks:      service.NewScheduler(d.Ingress, d.ServicePrefix, domain.SyncEvery),
		CallTimeout: s.CallTimeout, SyncEvery: domain.SyncEvery,
		Log: d.Log, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	watch := worker.ListingWatch{Prefix: d.ServicePrefix, Chotot: svc}
	for kind, h := range map[events.Kind]string{
		events.ListingSold: "Sold", events.ListingUpdated: "Updated", events.ListingDeleted: "Deleted",
		events.AccountDeleted: "AccountDeleted",
	} {
		d.Events.Subscribe(kind, watch.ServiceName(), h)
	}
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(watch)), Service: svc}
}

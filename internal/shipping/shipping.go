// Package shipping is a seller's accounts at GHTK and GHN and the carriers'
// reports. Module deal makes, reads and cancels shipments through it;
// shipping never imports deal, and reaches it through Settings.Deals.
package shipping

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/secret"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/handler"
	"github.com/shopnexus/server/internal/shipping/repository"
	"github.com/shopnexus/server/internal/shipping/service"
	"github.com/shopnexus/server/internal/shipping/worker"
)

// Settings is what shipping needs beyond the shared dependencies.
type Settings struct {
	// Clients holds a client for each carrier this stack runs; a carrier
	// off has none.
	Clients map[carrier.Carrier]carrier.Client
	// Tokens seals the carrier tokens under CARRIER_TOKEN_KEY.
	Tokens *secret.Box
	// WebhookSecrets is the secret in each carrier's webhook path; a
	// carrier without one has no webhook.
	WebhookSecrets map[carrier.Carrier]string
	Deals          service.Deals
	// DealNotFound is deal's error for a code no order has.
	DealNotFound error
}

// Mount registers the routes, the dev endpoint only while a carrier is the
// fake, subscribes the account watch to account.deleted, and returns
// shipping's Restate services and Service.
func Mount(d module.Deps, s Settings, customers accountsvc.Service) module.Mounted[service.Service] {
	fake := false
	for _, cl := range s.Clients {
		_, isFake := cl.(carrier.Fake)
		fake = fake || isFake
	}
	svc := service.New(service.Deps{Settings: repository.NewSettings(d.Pool), Customers: customers, Accounts: repository.NewAccounts(d.Pool), Events: repository.NewEvents(d.Pool), Clients: s.Clients, Tokens: s.Tokens, Deals: s.Deals, DealNotFound: s.DealNotFound, Now: d.Now})
	handler.New(svc, s.WebhookSecrets, d.Log).Register(d.Router, d.Protect, fake)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Shipping: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(watch)}, Service: svc}
}

// Package shipping is a seller's accounts at GHTK and GHN and the carriers'
// reports. Module deal makes, reads and cancels shipments through it;
// shipping never imports deal, and reaches it through Settings.Deals.
package shipping

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/carrier"
	"github.com/shopnexus/server/internal/shipping/handler"
	"github.com/shopnexus/server/internal/shipping/repository"
	"github.com/shopnexus/server/internal/shipping/service"
	"github.com/shopnexus/server/internal/shipping/worker"
)

// Settings is what shipping needs beyond the shared dependencies.
type Settings struct {
	// Clients holds one client per carrier; empty is CARRIER=none.
	Clients map[carrier.Carrier]carrier.Client
	// Key seals the carrier tokens, CARRIER_TOKEN_KEY.
	Key []byte
	// WebhookSecrets is the secret in each carrier's webhook path; a
	// carrier without one has no webhook.
	WebhookSecrets map[carrier.Carrier]string
	// FakeCarrier tells whether the carriers are the fake, whose parcels
	// the dev endpoint moves.
	FakeCarrier bool
	Deals       service.Deals
}

// Mount registers the routes, subscribes the account watch to
// account.deleted, and returns shipping's Restate services and Service.
func Mount(d module.Deps, s Settings, customers accountsvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{Settings: repository.NewSettings(d.Pool), Customers: customers, Accounts: repository.NewAccounts(d.Pool), Events: repository.NewEvents(d.Pool), Clients: s.Clients, Key: s.Key, Deals: s.Deals, Now: d.Now})
	handler.New(svc, s.WebhookSecrets, d.Log).Register(d.Router, d.Protect, s.FakeCarrier)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Shipping: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(watch)}, Service: svc}
}

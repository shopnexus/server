// Package shipping is a seller's accounts at GHTK and GHN and the carriers'
// reports. Module deal makes, reads and cancels shipments through it;
// shipping never imports deal, and reaches it through Settings.Deals.
package shipping

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/carrier"
	"github.com/shopnexus/server2/internal/shipping/handler"
	"github.com/shopnexus/server2/internal/shipping/repository"
	"github.com/shopnexus/server2/internal/shipping/service"
	"github.com/shopnexus/server2/internal/shipping/worker"
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
	Deals          service.Deals
}

// Mount registers the routes, subscribes the account watch to
// account.deleted, and returns shipping's Restate services and Service.
func Mount(d module.Deps, s Settings) module.Mounted[service.Service] {
	svc := service.New(service.Deps{Accounts: repository.NewAccounts(d.Pool), Clients: s.Clients, Key: s.Key, Deals: s.Deals, Now: d.Now})
	handler.New(svc, s.WebhookSecrets, d.Log).Register(d.Router, d.Protect)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Shipping: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(watch)}, Service: svc}
}

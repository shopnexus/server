// Package deal is a buyer and a seller agreeing a price on a listing.
package deal

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/deal/domain"
	"github.com/shopnexus/server2/internal/deal/handler"
	"github.com/shopnexus/server2/internal/deal/repository"
	"github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/deal/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Settings is what deal needs beyond the shared dependencies: module
// shipping, which makes and reads shipments on the seller's accounts,
// whether the carriers are the fake, whose shipments the dev endpoint
// moves, and how often a shipped order's shipment is read again.
type Settings struct {
	Shipping    service.Shipping
	FakeCarrier bool
	PollEvery   time.Duration
}

// Mount registers the deal endpoints, subscribes the listing watch to the
// listing events, the account watch to account.deleted and the chat post
// to deal.moved, and returns deal's Restate services and the Service other
// modules read deals through.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, catalog catalogsvc.Service,
	chats chatsvc.Service,
) module.Mounted[service.Service] {
	waits := domain.OrderWaits
	waits.Poll = s.PollEvery
	svc := service.New(service.Deps{
		Deals: repository.NewDeals(d.Pool), Accounts: accounts, Catalog: catalog, Chats: chats, Shipping: s.Shipping,
		Waits: waits, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, s.FakeCarrier)
	watch := worker.ListingWatch{Prefix: d.ServicePrefix, Deals: svc}
	for _, k := range []events.Kind{events.ListingUpdated, events.ListingHidden, events.ListingSold, events.ListingDeleted} {
		d.Events.Subscribe(k, watch.ServiceName(), "Changed")
	}
	accountWatch := worker.AccountWatch{Prefix: d.ServicePrefix, Deals: svc}
	d.Events.Subscribe(events.AccountDeleted, accountWatch.ServiceName(), "Deleted")
	post := worker.ChatPost{Prefix: d.ServicePrefix, Deals: svc}
	d.Events.Subscribe(events.DealMoved, post.ServiceName(), "Moved")
	return module.Mounted[service.Service]{
		Services: append(svc.Durable(), restate.Reflect(watch), restate.Reflect(accountWatch), restate.Reflect(post)),
		Service:  svc,
	}
}

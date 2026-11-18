// Package support holds what people ask ShopNexus: tickets, each its own
// chat with ShopNexus, which console members answer through console's API.
package support

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/support/handler"
	"github.com/shopnexus/server/internal/support/repository"
	"github.com/shopnexus/server/internal/support/service"
	"github.com/shopnexus/server/internal/support/worker"
)

// Mount registers the /v1/support endpoints, subscribes the worker to
// message.sent and account.deleted, and returns support's Restate services
// and its Service, which console and screen reach tickets through.
func Mount(d module.Deps, accounts accountsvc.Service, chats chatsvc.Service, deals service.Deals,
	listings service.Listings,
) module.Mounted[service.Service] {
	tickets := repository.NewTickets(d.Pool)
	svc := service.New(service.Deps{
		Tickets: tickets, Accounts: accounts, Chats: chats, Deals: deals, Listings: listings,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	w := worker.Worker{Prefix: d.ServicePrefix, Chats: chats, Support: svc}
	d.Events.Subscribe(events.MessageSent, w.ServiceName(), "MessageSent")
	d.Events.Subscribe(events.AccountDeleted, w.ServiceName(), "AccountDeleted")
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(w)), Service: svc}
}

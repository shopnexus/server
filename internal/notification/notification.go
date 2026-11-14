// Package notification tells people what happened to their deals, orders,
// listings, reviews and account, of new listings by the sellers they follow
// and by their search watches, and of price drops on their saves, in a list
// and by push, from the events of the other modules. Each person's settings
// say which kinds push and when pushes wait for morning.
// Only screen calls it, for Home's unread count.
package notification

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	discoverysvc "github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/notification/handler"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/notification/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/push"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// followerPage is how many followers one step of ListingPublished reads.
const followerPage = 500

// Settings is what notification's Mount takes beyond module.Deps.
type Settings struct {
	// Push delivers the pushes.
	Push push.Sender
}

// Mount registers the notification endpoints and subscribes the worker to
// the events it tells of, which it reads through the other modules'
// services.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, catalog catalogsvc.Service,
	deals dealsvc.Service, trust trustsvc.Service, chats chatsvc.Service, discovery discoverysvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{Notices: repository.NewNotices(d.Pool), Settings: repository.NewSettings(d.Pool), Now: d.Now})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	w := worker.Worker{
		Prefix: d.ServicePrefix, Notices: svc, Deals: deals, Chats: chats, Catalog: catalog,
		Trust: trust, Accounts: accounts, Discovery: discovery, Push: s.Push, Log: d.Log, FollowerPage: followerPage, Now: d.Now,
	}
	for kind, h := range map[events.Kind]string{
		events.DealMoved: "DealMoved", events.MessageSent: "MessageSent", events.ListingPublished: "ListingPublished",
		events.ListingRejected: "ListingRejected", events.ReviewPosted: "ReviewPosted",
		events.AccountSuspended: "AccountSuspended", events.AccountUnsuspended: "AccountUnsuspended",
		events.AccountDeleted: "AccountDeleted", events.SearchWatchMatched: "SearchWatchMatched",
		events.SavedPriceDropped: "SavedPriceDropped",
	} {
		d.Events.Subscribe(kind, w.ServiceName(), h)
	}
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(w)}, Service: svc}
}

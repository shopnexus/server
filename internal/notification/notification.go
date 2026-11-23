// Package notification tells people what happened to their deals, orders,
// listings, reviews and account, of new listings by the sellers they follow
// and by their search watches, and of price drops on their saves, in a list,
// by push and, for orders and the account, by mail, from the events of the
// other modules. Each person's settings say which kinds push, when pushes
// wait for morning and whether order notices are mailed.
// Only screen calls it, for Home's unread count.
package notification

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/notification/handler"
	"github.com/shopnexus/server/internal/notification/repository"
	"github.com/shopnexus/server/internal/notification/service"
	"github.com/shopnexus/server/internal/notification/worker"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/email"
	"github.com/shopnexus/server/internal/provider/push"
	supportsvc "github.com/shopnexus/server/internal/support/service"
	trustsvc "github.com/shopnexus/server/internal/trust/service"
)

// followerPage is how many followers one step of ListingPublished reads.
const followerPage = 500

// Settings is what notification's Mount takes beyond module.Deps.
type Settings struct {
	// Push delivers the pushes.
	Push push.Sender
	// Email delivers the mails, whose links go to Site, the website's
	// origin.
	Email email.Sender
	Site  string
}

// Mount registers the notification endpoints and subscribes the worker to
// the events it tells of, which it reads through the other modules'
// services.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, catalog catalogsvc.Service,
	deals dealsvc.Service, trust trustsvc.Service, chats chatsvc.Service, discovery discoverysvc.Service,
	support supportsvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{Notices: repository.NewNotices(d.Pool), Settings: repository.NewSettings(d.Pool), Now: d.Now})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	w := worker.Worker{
		Prefix: d.ServicePrefix, Notices: svc, Deals: deals, Chats: chats, Catalog: catalog,
		Trust: trust, Accounts: accounts, Discovery: discovery, Support: support, Push: s.Push, Email: s.Email, Site: s.Site, Log: d.Log, FollowerPage: followerPage, Now: d.Now,
	}
	for kind, h := range map[events.Kind]string{
		events.DealMoved: "DealMoved", events.MessageSent: "MessageSent", events.ListingPublished: "ListingPublished",
		events.ListingRejected: "ListingRejected", events.ReviewPosted: "ReviewPosted",
		events.ReviewReplied:    "ReviewReplied",
		events.AccountSuspended: "AccountSuspended", events.AccountUnsuspended: "AccountUnsuspended",
		events.AccountDeleted: "AccountDeleted", events.SearchWatchMatched: "SearchWatchMatched",
		events.SavedPriceDropped: "SavedPriceDropped",
	} {
		d.Events.Subscribe(kind, w.ServiceName(), h)
	}
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(w)}, Service: svc}
}

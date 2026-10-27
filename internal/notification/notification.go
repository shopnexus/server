// Package notification tells people what happened to their deals, orders,
// listings, reviews and account, in a list and by push, from the events of
// the other modules. No module calls it.
package notification

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/notification/handler"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/notification/worker"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/push"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// Mounted is what notification's Mount gives app.
type Mounted struct {
	Services []restate.ServiceDefinition
	Service  service.Service
}

// Sources are the modules whose changes notification tells of.
type Sources struct {
	Accounts accountsvc.Service
	Auth     authsvc.Service
	Catalog  catalogsvc.Service
	Deals    dealsvc.Service
	Chats    chatsvc.Service
	Trust    trustsvc.Service
}

// Mount registers the notification endpoints behind protect and subscribes
// the worker to the events it tells of; sender delivers the pushes.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, src Sources, sender push.Sender) Mounted {
	notices := repository.NewNotices(d.Pool)
	svc := service.New(service.Deps{Notices: notices, Now: d.Now})
	handler.New(svc, d.Log).Register(d.Router, protect)
	w := worker.Worker{
		Prefix: d.ServicePrefix, Notices: svc, Deals: src.Deals, Chats: src.Chats, Catalog: src.Catalog,
		Trust: src.Trust, Accounts: src.Accounts, Auth: src.Auth, Push: sender, Log: d.Log,
	}
	for kind, h := range map[events.Kind]string{
		events.DealMoved: "DealMoved", events.MessageSent: "MessageSent", events.ListingPublished: "ListingPublished",
		events.ListingRejected: "ListingRejected", events.ReviewPosted: "ReviewPosted",
		events.AccountSuspended: "AccountSuspended", events.AccountUnsuspended: "AccountUnsuspended",
	} {
		d.Events.Subscribe(kind, w.ServiceName(), h)
	}
	return Mounted{Services: []restate.ServiceDefinition{restate.Reflect(w)}, Service: svc}
}

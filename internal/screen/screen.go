// Package screen serves one read per view of the app, for mobile and
// desktop alike. It owns no table: each view reads the other modules'
// services, and no module calls screen.
package screen

import (
	"net/http"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	discoverysvc "github.com/shopnexus/server2/internal/discovery/service"
	notificationsvc "github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/screen/handler"
	"github.com/shopnexus/server2/internal/screen/service"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// Guards are the two ways a view reads its caller: protect requires one,
// identify reads one when the request carries it.
type Guards struct {
	Protect, Identify func(http.Handler) http.Handler
}

// Mount registers the screen endpoints.
func Mount(d module.Deps, g Guards, accounts accountsvc.Service, catalog catalogsvc.Service,
	deals dealsvc.Service, trust trustsvc.Service, saves discoverysvc.Service, chats chatsvc.Service,
	notifications notificationsvc.Service,
) {
	svc := service.New(service.Deps{
		Accounts: accounts, Catalog: catalog, Deals: deals, Trust: trust, Saves: saves, Chats: chats,
		Notifications: notifications,
	})
	handler.New(svc, d.Log).Register(d.Router, g.Protect, g.Identify)
}

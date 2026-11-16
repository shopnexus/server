// Package screen serves one read per view of the app, for mobile and
// desktop alike. It owns no table: each view reads the other modules'
// services, and no module calls screen.
package screen

import (
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	chatsvc "github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	discoverysvc "github.com/shopnexus/server2/internal/discovery/service"
	notificationsvc "github.com/shopnexus/server2/internal/notification/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/screen/handler"
	"github.com/shopnexus/server2/internal/screen/service"
	shippingsvc "github.com/shopnexus/server2/internal/shipping/service"
	supportsvc "github.com/shopnexus/server2/internal/support/service"
	trustsvc "github.com/shopnexus/server2/internal/trust/service"
)

// Mount registers the screen endpoints.
func Mount(d module.Deps, accounts accountsvc.Service, catalog catalogsvc.Service,
	deals dealsvc.Service, trust trustsvc.Service, discovery discoverysvc.Service, chats chatsvc.Service,
	notifications notificationsvc.Service, support supportsvc.Service, shipping shippingsvc.Service,
) {
	svc := service.New(service.Deps{
		Accounts: accounts, Catalog: catalog, Deals: deals, Trust: trust, Discovery: discovery, Chats: chats,
		Notifications: notifications, Support: support, Shipping: shipping, Now: d.Now, Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
}

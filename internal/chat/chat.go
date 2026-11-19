// Package chat is the conversation of two people, whatever their roles,
// with their messages.
package chat

import (
	"net/url"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chat/handler"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/chat/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/module"
)

// Settings are what chat reads links in messages with: the website's
// origins and the orders a link may point at.
type Settings struct {
	WebsiteOrigins []string
	Orders         service.OrderRefs
}

// Mount registers the chat endpoints and returns chat's
// Restate services and its Service, which screen reads conversations
// through.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, catalog catalogsvc.Service,
	media mediasvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Chats: repository.NewChats(d.Pool), Accounts: accounts, Catalog: catalog, Media: media,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, LinkHosts: hostsOf(s.WebsiteOrigins), Orders: s.Orders,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	return module.Mounted[service.Service]{Services: svc.Durable(), Service: svc}
}

// hostsOf is the host of each origin, such as shopnexus.id.vn of
// https://shopnexus.id.vn; one that does not parse is left out.
func hostsOf(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if u, err := url.Parse(o); err == nil && u.Hostname() != "" {
			out = append(out, u.Hostname())
		}
	}
	return out
}

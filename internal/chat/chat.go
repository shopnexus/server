// Package chat is the conversation of two people, whatever their roles,
// with their messages.
package chat

import (
	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chat/handler"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/chat/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/module"
)

// Mount registers the chat endpoints and returns chat's
// Restate services and its Service, which screen reads conversations
// through.
func Mount(d module.Deps, accounts accountsvc.Service, catalog catalogsvc.Service,
	media mediasvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Chats: repository.NewChats(d.Pool), Accounts: accounts, Catalog: catalog, Media: media,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	return module.Mounted[service.Service]{Services: svc.Durable(), Service: svc}
}

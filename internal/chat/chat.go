// Package chat is the conversation of two people, whatever their roles,
// with their messages and their deals.
package chat

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/chat/handler"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/chat/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Mount registers the chat endpoints behind protect and returns chat's
// Restate services.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, accounts accountsvc.Service, auth authsvc.Service,
	catalog catalogsvc.Service, media mediasvc.Service, deals dealsvc.Service,
) []restate.ServiceDefinition {
	svc := service.New(service.Deps{
		Chats: repository.NewChats(d.Pool), Accounts: accounts, Auth: auth, Catalog: catalog, Media: media, Deals: deals,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, protect)
	return svc.Durable()
}

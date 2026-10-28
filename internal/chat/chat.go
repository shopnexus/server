// Package chat is the conversation of two people, whatever their roles,
// with their messages.
package chat

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/chat/handler"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/chat/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Mounted is what chat's Mount gives app.
type Mounted struct {
	Services []restate.ServiceDefinition
	Service  service.Service
}

// Mount registers the chat endpoints behind protect and returns chat's
// Restate services and its Service, which screen reads conversations
// through.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, accounts accountsvc.Service,
	catalog catalogsvc.Service, media mediasvc.Service,
) Mounted {
	svc := service.New(service.Deps{
		Chats: repository.NewChats(d.Pool), Accounts: accounts, Catalog: catalog, Media: media,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, protect)
	return Mounted{Services: svc.Durable(), Service: svc}
}

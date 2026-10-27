// Package catalog is what sellers offer: listings from posting to sale,
// their categories, and the communes they are offered in.
package catalog

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	"github.com/shopnexus/server2/internal/catalog/handler"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Guards are the auth middlewares catalog's routes sit behind.
type Guards struct {
	// Protect requires a signed-in caller.
	Protect func(http.Handler) http.Handler
	// Identify lets anyone through and tells the handler who signed in.
	Identify func(http.Handler) http.Handler
}

// Mounted is what catalog's Mount gives app and other modules.
type Mounted struct {
	Services []restate.ServiceDefinition
	Service  service.Service
}

// Mount registers the catalog endpoints and returns catalog's Restate
// services and the Service other modules read listings through.
func Mount(d module.Deps, g Guards, accounts accountsvc.Service, auth authsvc.Service,
	media mediasvc.Service,
) Mounted {
	svc := service.New(service.Deps{
		Listings: repository.NewListings(d.Pool), Categories: repository.NewCategories(d.Pool),
		Regions: repository.NewRegions(d.Pool), Searches: repository.NewSearches(d.Pool), Accounts: accounts, Auth: auth, Media: media,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, g.Protect, g.Identify)
	return Mounted{Services: svc.Durable(), Service: svc}
}

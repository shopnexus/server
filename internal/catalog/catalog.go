// Package catalog is what sellers offer: listings from posting to sale,
// their categories, and the communes they are offered in.
package catalog

import (
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/handler"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Mount registers the catalog endpoints and returns catalog's Restate
// services and the Service other modules read listings through.
func Mount(d module.Deps, accounts accountsvc.Service, media mediasvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Listings: repository.NewListings(d.Pool), Categories: repository.NewCategories(d.Pool),
		Regions: repository.NewRegions(d.Pool), Searches: repository.NewSearches(d.Pool), Accounts: accounts, Media: media,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	return module.Mounted[service.Service]{Services: svc.Durable(), Service: svc}
}

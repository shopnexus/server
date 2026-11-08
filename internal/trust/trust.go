// Package trust is what people can rely on about each other: for now the
// reviews buyers give sellers and the rating they add up to.
package trust

import (
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/trust/handler"
	"github.com/shopnexus/server2/internal/trust/repository"
	"github.com/shopnexus/server2/internal/trust/service"
)

// Mount registers the trust endpoints and returns the Service screen reads
// ratings and reviews through.
func Mount(d module.Deps, accounts accountsvc.Service, catalog catalogsvc.Service, deals dealsvc.Service) service.Service {
	svc := service.New(service.Deps{
		Reviews: repository.NewReviews(d.Pool), Accounts: accounts, Catalog: catalog, Deals: deals, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	return svc
}

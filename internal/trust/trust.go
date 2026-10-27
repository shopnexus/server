// Package trust is what people can rely on about each other: for now the
// reviews buyers give sellers.
package trust

import (
	"net/http"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/trust/handler"
	"github.com/shopnexus/server2/internal/trust/repository"
	"github.com/shopnexus/server2/internal/trust/service"
)

// Mount registers the trust endpoints, the writing ones behind protect.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, accounts accountsvc.Service,
	catalog catalogsvc.Service, deals dealsvc.Service,
) {
	svc := service.New(service.Deps{
		Reviews: repository.NewReviews(d.Pool), Accounts: accounts, Catalog: catalog, Deals: deals, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, protect)
}

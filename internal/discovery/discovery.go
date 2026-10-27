// Package discovery is how people find and keep listings: for now the
// listings they save.
package discovery

import (
	"net/http"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/handler"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Mount registers the discovery endpoints behind protect and returns the
// Service screen reads saves through.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, catalog catalogsvc.Service) service.Service {
	svc := service.New(service.Deps{Saves: repository.NewSaves(d.Pool), Catalog: catalog, Now: d.Now})
	handler.New(svc, d.Log).Register(d.Router, protect)
	return svc
}

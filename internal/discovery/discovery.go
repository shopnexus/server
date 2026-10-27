// Package discovery is how people find and keep listings: search, the
// nearby feed, and the listings they save.
package discovery

import (
	"net/http"

	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/handler"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Guards are the two ways an endpoint reads its caller: protect requires
// one, identify reads one when the request carries it.
type Guards struct {
	Protect, Identify func(http.Handler) http.Handler
}

// Mount registers the discovery endpoints and returns the Service screen
// reads saves and the nearby feed through. model reads search queries.
func Mount(d module.Deps, g Guards, catalog catalogsvc.Service, model llm.Client) service.Service {
	svc := service.New(service.Deps{
		Saves: repository.NewSaves(d.Pool), Understandings: repository.NewUnderstandings(d.Pool), Catalog: catalog,
		LLM: model, ModelTimeout: domain.ModelTimeout, Log: d.Log, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, g.Protect, g.Identify)
	return svc
}

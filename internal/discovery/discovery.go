// Package discovery is how people find and keep listings: search, the
// nearby feed, and the listings they save.
package discovery

import (
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/handler"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Settings is what discovery's Mount takes beyond module.Deps.
type Settings struct {
	// Model reads search queries.
	Model llm.Client
}

// Mount registers the discovery endpoints and returns the Service screen
// reads saves and the nearby feed through.
func Mount(d module.Deps, s Settings, catalog catalogsvc.Service) service.Service {
	svc := service.New(service.Deps{
		Saves: repository.NewSaves(d.Pool), Understandings: repository.NewUnderstandings(d.Pool), Catalog: catalog,
		LLM: s.Model, ModelTimeout: domain.ModelTimeout, Log: d.Log, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	return svc
}

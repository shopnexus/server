// Package discovery is how people find and keep listings: search, with the
// nearby feed as one of its sorts, and the listings they save.
package discovery

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/handler"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	"github.com/shopnexus/server2/internal/discovery/worker"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Settings is what discovery's Mount takes beyond module.Deps.
type Settings struct {
	// Model reads search queries.
	Model llm.Client
	// Text and Image embed with the search key, under the versions; the
	// floors are the cosine a listing needs to join a vector list.
	Text, Image               embed.Model
	TextVersion, ImageVersion string
	TextMin, PhotoMin         float64
}

// Mount registers the discovery endpoints, subscribes the account watch to
// account.deleted, and returns discovery's Restate services and the
// Service screen reads saves and Home's nearby feed through.
func Mount(
	d module.Deps, s Settings, catalog catalogsvc.Service, media mediasvc.Service, accounts accountsvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Saves: repository.NewSaves(d.Pool), Understandings: repository.NewUnderstandings(d.Pool), History: repository.NewHistory(d.Pool),
		Hidden: repository.NewHidden(d.Pool), Settings: repository.NewSettings(d.Pool), Accounts: accounts,
		Watches: repository.NewWatches(d.Pool), Sweeps: service.NewSweeper(d.Ingress, d.ServicePrefix),
		ArrivalLag: domain.ArrivalLag, SweepBatch: domain.SweepBatch,
		QueryVectors: repository.NewQueryVectors(d.Pool), PhotoVectors: repository.NewPhotoVectors(d.Pool),
		Catalog: catalog, Media: media, LLM: s.Model, Text: s.Text, Image: s.Image,
		TextVersion: s.TextVersion, ImageVersion: s.ImageVersion, TextMin: s.TextMin, PhotoMin: s.PhotoMin,
		ModelTimeout: domain.ModelTimeout, Log: d.Log, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Discovery: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(watch)}, Service: svc}
}

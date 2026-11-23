// Package discovery is how people find and keep listings: search, with the
// nearby feed as one of its sorts, and the listings they save.
package discovery

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/discovery/domain"
	"github.com/shopnexus/server/internal/discovery/handler"
	"github.com/shopnexus/server/internal/discovery/repository"
	"github.com/shopnexus/server/internal/discovery/service"
	"github.com/shopnexus/server/internal/discovery/worker"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/llm"
)

// Settings is what discovery's Mount takes beyond module.Deps.
type Settings struct {
	// Model reads search queries.
	Model llm.Client
	// Text and Image embed with the search key, under the versions; the
	// floors are the cosine a listing needs to join a vector list, LookMin
	// the photo list's when a search's look is the query.
	Text, Image                embed.Model
	TextVersion, ImageVersion  string
	TextMin, PhotoMin, LookMin float64
}

// Mount registers the discovery endpoints, subscribes the account watch to
// account.deleted and the listing watch to listing.published and
// listing.updated, and returns discovery's Restate services, the sweep of
// the search watches among them, and the Service screen reads saves and
// Home's nearby feed through.
func Mount(
	d module.Deps, s Settings, catalog catalogsvc.Service, media mediasvc.Service, accounts accountsvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Saves: repository.NewSaves(d.Pool), Views: repository.NewViews(d.Pool), Understandings: repository.NewUnderstandings(d.Pool), History: repository.NewHistory(d.Pool),
		Hidden: repository.NewHidden(d.Pool), Accounts: accounts,
		Watches: repository.NewWatches(d.Pool), Sweeps: service.NewSweeper(d.Ingress, d.ServicePrefix),
		ArrivalLag: domain.ArrivalLag, SweepBatch: domain.SweepBatch,
		QueryVectors: repository.NewQueryVectors(d.Pool), PhotoVectors: repository.NewPhotoVectors(d.Pool),
		Catalog: catalog, Media: media, LLM: s.Model, Text: s.Text, Image: s.Image,
		TextVersion: s.TextVersion, ImageVersion: s.ImageVersion, TextMin: s.TextMin, PhotoMin: s.PhotoMin, LookMin: s.LookMin,
		ModelTimeout: domain.ModelTimeout, Log: d.Log, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Discovery: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	listings := worker.ListingWatch{Prefix: d.ServicePrefix, Discovery: svc}
	d.Events.Subscribe(events.ListingPublished, listings.ServiceName(), "Published")
	d.Events.Subscribe(events.ListingUpdated, listings.ServiceName(), "Updated")
	sweep := worker.SearchWatchSweep{Prefix: d.ServicePrefix, Discovery: svc, Every: domain.SweepEvery}
	vectors := worker.SearchVectorSweep{Prefix: d.ServicePrefix, Discovery: svc}
	return module.Mounted[service.Service]{
		Services: []restate.ServiceDefinition{
			restate.Reflect(watch), restate.Reflect(listings), restate.Reflect(sweep), restate.Reflect(vectors),
		},
		Service: svc,
	}
}

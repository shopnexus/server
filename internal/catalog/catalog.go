// Package catalog is what sellers offer: listings from posting to sale,
// their categories, and the communes they are offered in.
package catalog

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/catalog/handler"
	"github.com/shopnexus/server/internal/catalog/repository"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/catalog/worker"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/embed"
)

// Settings is what catalog's Mount takes beyond module.Deps: the embedding
// models ListingEmbed calls with the index key, and IndexNow.
type Settings struct {
	Text, Image               embed.Model
	TextVersion, ImageVersion string
	EmbedSlots                int
	IndexNow                  service.IndexNowSettings
}

// indexNowKinds are the listing changes a search engine would see: on
// sale, edited, off sale, sold (410 on the website) and deleted (404).
var indexNowKinds = []events.Kind{
	events.ListingPublished, events.ListingUpdated, events.ListingHidden, events.ListingUnhidden,
	events.ListingSold, events.ListingUnsold, events.ListingDeleted,
}

// Mount registers the catalog endpoints, subscribes the account watch to
// account.deleted and, with IndexNow on, the listing watch to
// indexNowKinds, and returns catalog's Restate services and the Service
// other modules read listings through.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, media mediasvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Listings: repository.NewListings(d.Pool), Drafts: repository.NewDrafts(d.Pool), Categories: repository.NewCategories(d.Pool),
		Regions: repository.NewRegions(d.Pool), Searches: repository.NewSearches(d.Pool), Vectors: repository.NewVectors(d.Pool),
		Accounts: accounts, Media: media, Text: s.Text, Image: s.Image, TextVersion: s.TextVersion, ImageVersion: s.ImageVersion,
		EmbedSlots: s.EmbedSlots, Log: d.Log, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, IndexNow: s.IndexNow,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	registerGauges(d)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Listings: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	listings := worker.ListingWatch{Prefix: d.ServicePrefix}
	if s.IndexNow.On() {
		for _, kind := range indexNowKinds {
			d.Events.Subscribe(kind, listings.ServiceName(), "Changed")
		}
	}
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(watch), restate.Reflect(listings)), Service: svc}
}

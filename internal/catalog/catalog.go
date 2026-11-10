// Package catalog is what sellers offer: listings from posting to sale,
// their categories, and the communes they are offered in.
package catalog

import (
	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/handler"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/catalog/worker"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/embed"
)

// Settings is what catalog's Mount takes beyond module.Deps: the embedding
// models ListingEmbed calls with the index key.
type Settings struct {
	Text, Image               embed.Model
	TextVersion, ImageVersion string
	EmbedSlots                int
}

// Mount registers the catalog endpoints, subscribes the account watch to
// account.deleted, and returns catalog's Restate services and the Service
// other modules read listings through.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, media mediasvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Listings: repository.NewListings(d.Pool), Categories: repository.NewCategories(d.Pool),
		Regions: repository.NewRegions(d.Pool), Searches: repository.NewSearches(d.Pool), Vectors: repository.NewVectors(d.Pool),
		Accounts: accounts, Media: media, Text: s.Text, Image: s.Image, TextVersion: s.TextVersion, ImageVersion: s.ImageVersion,
		EmbedSlots: s.EmbedSlots, Log: d.Log, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Listings: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(watch)), Service: svc}
}

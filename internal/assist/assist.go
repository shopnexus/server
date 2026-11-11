// Package assist is AI that helps a person do something in the app. Today
// it reads a seller's video or their listing on Chợ Tốt into a listing
// draft: photos, the fields and a price range.
package assist

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/assist/handler"
	"github.com/shopnexus/server2/internal/assist/repository"
	"github.com/shopnexus/server2/internal/assist/service"
	"github.com/shopnexus/server2/internal/assist/worker"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Settings is what assist's Mount takes beyond module.Deps: the models a
// read calls, the search key's embedding models, and search's floors.
type Settings struct {
	Frames            service.FrameCutter
	STT               service.Transcriber
	Model             llm.Client
	Text, Image       embed.Model
	TextMin, PhotoMin float64
	CallTimeout       time.Duration
	// Source is Chợ Tốt, for link reads.
	Source service.ListingSource
}

// Mount registers the assist endpoints, subscribes the account watch to
// account.deleted, and returns assist's Restate services and its Service.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, media mediasvc.Service, catalog catalogsvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Reads: repository.NewListingReads(d.Pool), Source: s.Source, Accounts: accounts, Media: media, Catalog: catalog,
		Frames: s.Frames, STT: s.STT, Model: s.Model, Text: s.Text, Image: s.Image,
		TextMin: s.TextMin, PhotoMin: s.PhotoMin, CallTimeout: s.CallTimeout,
		Log: d.Log, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Reads: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(watch)), Service: svc}
}

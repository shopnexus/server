// Package assist is AI that helps a person do something in the app. It
// reads a seller's video or photo into a listing draft (photos, the fields
// and a price range), drafts a Chợ Tốt listing for module chotot, and
// prices a draft against similar listings and the buyers watching for one.
package assist

import (
	"time"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	aisvc "github.com/shopnexus/server/internal/ai/service"
	"github.com/shopnexus/server/internal/assist/handler"
	"github.com/shopnexus/server/internal/assist/repository"
	"github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/assist/worker"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/llm"
)

// Settings is what assist's Mount takes beyond module.Deps: the models a
// read calls, the search key's embedding models, and search's floors.
type Settings struct {
	// AI cuts a video's frames and hears it.
	AI aisvc.Service
	// Model writes a read's text; Relay carries it to the seller.
	Model             llm.Streamer
	Relay             service.Relay
	Text, Image       embed.Model
	TextMin, PhotoMin float64
	CallTimeout       time.Duration
}

// Mount registers the assist endpoints, subscribes the account watch to
// account.deleted, and returns assist's Restate services and its Service.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, media mediasvc.Service, catalog catalogsvc.Service,
	discovery discoverysvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Reads: repository.NewListingReads(d.Pool), Accounts: accounts, Media: media, Catalog: catalog,
		Discovery: discovery,
		AI:        s.AI, Model: s.Model, Relay: s.Relay, Text: s.Text, Image: s.Image,
		TextMin: s.TextMin, PhotoMin: s.PhotoMin, CallTimeout: s.CallTimeout,
		Log: d.Log, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	watch := worker.AccountWatch{Prefix: d.ServicePrefix, Reads: svc}
	d.Events.Subscribe(events.AccountDeleted, watch.ServiceName(), "Deleted")
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(watch)), Service: svc}
}

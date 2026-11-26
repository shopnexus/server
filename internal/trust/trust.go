// Package trust is what people can rely on about each other: the reviews
// buyers give sellers, the rating they add up to, the reports people make
// of listings and accounts, and the scam warnings of chat.
package trust

import (
	"log/slog"
	"time"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	chatsvc "github.com/shopnexus/server/internal/chat/service"
	dealsvc "github.com/shopnexus/server/internal/deal/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/provider/llm"
	"github.com/shopnexus/server/internal/trust/handler"
	"github.com/shopnexus/server/internal/trust/repository"
	"github.com/shopnexus/server/internal/trust/service"
	"github.com/shopnexus/server/internal/trust/worker"
)

// Settings is what trust's Mount takes beyond module.Deps: the scam
// classifier, and search's cosine floors for the price signal.
type Settings struct {
	Classifier service.Classifier
	// ScamChecks off leaves message.sent unsubscribed: no message reaches
	// the classifier.
	ScamChecks        bool
	TextMin, PhotoMin float64
}

// LLMClassifier is the scam classifier that asks model under alias, each
// call within timeout. A trained model gets a constructor of its own here,
// and app passes it instead.
func LLMClassifier(model llm.Client, alias string, timeout time.Duration, log *slog.Logger) service.Classifier {
	return service.LLMClassifier{Model: model, Alias: alias, Timeout: timeout, Log: log}
}

// Mount registers the trust endpoints, subscribes the scam worker to
// message.sent (unless s turns scam checks off) and account.deleted, and returns trust's Restate services and
// the Service screen reads ratings, reviews and warnings through.
func Mount(d module.Deps, s Settings, accounts accountsvc.Service, catalog catalogsvc.Service, deals dealsvc.Service,
	chats chatsvc.Service, media mediasvc.Service,
) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Reviews: repository.NewReviews(d.Pool), Reports: repository.NewReports(d.Pool), Cases: repository.NewCases(d.Pool), Scams: repository.NewScamWarnings(d.Pool),
		Accounts: accounts, Catalog: catalog, Deals: deals, Chats: chats, Media: media, Classifier: s.Classifier,
		Facts: []service.FactSource{
			service.AccountAge{Accounts: accounts},
			service.MarketGap{Catalog: catalog, TextMin: s.TextMin, PhotoMin: s.PhotoMin},
		},
		Log: d.Log, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	registerGauges(d)
	w := worker.ScamWorker{Prefix: d.ServicePrefix, Chats: chats, Trust: svc}
	if s.ScamChecks {
		d.Events.Subscribe(events.MessageSent, w.ServiceName(), "MessageSent")
	}
	d.Events.Subscribe(events.AccountDeleted, w.ServiceName(), "AccountDeleted")
	return module.Mounted[service.Service]{Services: append(svc.Durable(), restate.Reflect(w)), Service: svc}
}

// Package feedback keeps what people tell ShopNexus without asking for an
// answer, unlike a support ticket. Staff work it through console's API, and
// notification tells the person what became of it.
package feedback

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/feedback/handler"
	"github.com/shopnexus/server/internal/feedback/repository"
	"github.com/shopnexus/server/internal/feedback/service"
	"github.com/shopnexus/server/internal/feedback/worker"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
)

// Mount registers the /v1/feedback endpoints, subscribes the worker to
// account.deleted, and returns its Restate service and the Service console
// and notification reach feedback through.
func Mount(d module.Deps, media mediasvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{Store: repository.NewStore(d.Pool), Media: media, Now: d.Now})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	w := worker.Worker{Prefix: d.ServicePrefix, Feedback: svc}
	d.Events.Subscribe(events.AccountDeleted, w.ServiceName(), "AccountDeleted")
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(w)}, Service: svc}
}

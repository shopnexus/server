// Package boost is the packages sellers buy to move a listing to the top
// of the newest order once a day. It opens payments through module payment
// and hears what became of them through payment.* events; catalog moves
// the listing up.
package boost

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/boost/domain"
	"github.com/shopnexus/server/internal/boost/handler"
	"github.com/shopnexus/server/internal/boost/repository"
	"github.com/shopnexus/server/internal/boost/service"
	"github.com/shopnexus/server/internal/boost/worker"
	paymentsvc "github.com/shopnexus/server/internal/payment/service"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/module"
)

// Settings is what boost needs beyond the shared dependencies.
type Settings struct {
	// Packages are BOOST_PACKAGES, shortest first.
	Packages []domain.Package
	// Moderator guards the console's read, as console builds it.
	Moderator func(http.Handler) http.Handler
}

// Mount registers the routes, subscribes the worker to the payment events
// and returns boost's Restate services and Service.
func Mount(d module.Deps, s Settings, payments paymentsvc.Service, catalog service.Catalog, accounts accountsvc.Service) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Store: repository.NewStore(d.Pool), Packages: s.Packages, Payments: payments, Catalog: catalog, Accounts: accounts,
		Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, Log: d.Log,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, s.Moderator)
	w := worker.Worker{Prefix: d.ServicePrefix, Boosts: svc, Now: d.Now}
	d.Events.Subscribe(events.PaymentSucceeded, w.ServiceName(), "Succeeded")
	d.Events.Subscribe(events.PaymentFailed, w.ServiceName(), "Failed")
	d.Events.Subscribe(events.PaymentExpired, w.ServiceName(), "Expired")
	return module.Mounted[service.Service]{
		Services: []restate.ServiceDefinition{restate.Reflect(w), restate.Reflect(service.Timers{Boosts: svc})},
		Service:  svc,
	}
}

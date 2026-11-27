// Package payment is the platform's own takings: one payment per purchase
// through a gateway, VNPay or its fake, and never given back.
// The modules that sell open payments through its Service and hear what
// became of them through payment.* events; payment never imports them.
package payment

import (
	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/payment/handler"
	"github.com/shopnexus/server/internal/payment/repository"
	"github.com/shopnexus/server/internal/payment/service"
	"github.com/shopnexus/server/internal/platform/module"
	provider "github.com/shopnexus/server/internal/provider/payment"
)

// Mount registers the gateway's routes, the dev page only with the fake,
// and returns payment's Restate service and Service. A nil gateway takes no
// payments.
func Mount(d module.Deps, gateway provider.Client) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Store: repository.NewStore(d.Pool), Gateway: gateway, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now, Log: d.Log,
	})
	_, fake := gateway.(provider.Fake)
	handler.New(svc, d.Log).Register(d.Router, fake)
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(service.Flows{Payments: svc})}, Service: svc}
}

// Package support holds what people send to ShopNexus: feedback, which
// console members read through console's API.
package support

import (
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/support/handler"
	"github.com/shopnexus/server2/internal/support/repository"
	"github.com/shopnexus/server2/internal/support/service"
)

// Mount registers POST /v1/feedback and returns the Service console reads
// feedback through.
func Mount(d module.Deps, accounts accountsvc.Service) service.Service {
	svc := service.New(service.Deps{Feedback: repository.NewFeedback(d.Pool), Accounts: accounts, Now: d.Now})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	return svc
}

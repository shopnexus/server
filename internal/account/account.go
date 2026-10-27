// Package account owns who a person is: display name, bio, avatar, status
// and suspension. It knows nothing about signing in; package auth does.
package account

import (
	"net/http"

	"github.com/shopnexus/server2/internal/account/handler"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/module"
)

// Mount registers the account endpoints behind protect, which requires a
// signed-in caller, and returns the Service other modules call. media
// makes avatars.
func Mount(d module.Deps, protect func(http.Handler) http.Handler, media mediasvc.Service) service.Service {
	svc := service.New(repository.NewAccounts(d.Pool), media, d.Now)
	handler.New(svc, d.Log).Register(d.Router, protect)
	return svc
}

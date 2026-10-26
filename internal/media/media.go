// Package media stores the photos clients upload: presigned PUTs to the
// object store, attaching them to what uses them, and processing them into
// public photos and thumbnails.
package media

import (
	"net/http"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/media/handler"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/platform/objectstore"
)

// Mounted is what media's Mount gives app and other modules.
type Mounted struct {
	Services []restate.ServiceDefinition
	Service  service.Service
}

// Mount registers the media endpoints behind protect and returns media's
// Restate services and the Service other modules attach uploads through.
func Mount(d module.Deps, store *objectstore.Store, protect func(http.Handler) http.Handler) Mounted {
	svc := service.New(service.Deps{
		Uploads: repository.NewUploads(d.Pool), Store: store, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, protect)
	return Mounted{Services: svc.Durable(), Service: svc}
}

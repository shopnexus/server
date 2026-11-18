// Package media stores the photos clients upload: presigned PUTs to the
// object store, attaching them to what uses them, and processing them into
// public photos and thumbnails.
package media

import (
	"github.com/shopnexus/server/internal/media/handler"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/module"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// Settings is what media's Mount takes beyond module.Deps.
type Settings struct {
	// Store holds the uploads and the processed photos.
	Store *objectstore.Store
}

// Mount registers the media endpoints and returns media's Restate services
// and the Service other modules attach uploads through.
func Mount(d module.Deps, s Settings) module.Mounted[service.Service] {
	svc := service.New(service.Deps{
		Uploads: repository.NewUploads(d.Pool), Store: s.Store, Ingress: d.Ingress, Prefix: d.ServicePrefix, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect)
	return module.Mounted[service.Service]{Services: svc.Durable(), Service: svc}
}

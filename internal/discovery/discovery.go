// Package discovery is how people find and keep listings: search, the
// nearby feed, and the listings they save.
package discovery

import (
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/discovery/domain"
	"github.com/shopnexus/server2/internal/discovery/handler"
	"github.com/shopnexus/server2/internal/discovery/repository"
	"github.com/shopnexus/server2/internal/discovery/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/module"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Settings is what discovery's Mount takes beyond module.Deps.
type Settings struct {
	// Model reads search queries.
	Model llm.Client
	// Text and Image embed with the search key, under the versions; the
	// floors are the cosine a listing needs to join a vector list.
	Text, Image               embed.Model
	TextVersion, ImageVersion string
	TextMin, PhotoMin         float64
}

// Mount registers the discovery endpoints and returns the Service screen
// reads saves and the nearby feed through.
func Mount(d module.Deps, s Settings, catalog catalogsvc.Service, media mediasvc.Service) service.Service {
	svc := service.New(service.Deps{
		Saves: repository.NewSaves(d.Pool), Understandings: repository.NewUnderstandings(d.Pool),
		QueryVectors: repository.NewQueryVectors(d.Pool), PhotoVectors: repository.NewPhotoVectors(d.Pool),
		Catalog: catalog, Media: media, LLM: s.Model, Text: s.Text, Image: s.Image,
		TextVersion: s.TextVersion, ImageVersion: s.ImageVersion, TextMin: s.TextMin, PhotoMin: s.PhotoMin,
		ModelTimeout: domain.ModelTimeout, Log: d.Log, Now: d.Now,
	})
	handler.New(svc, d.Log).Register(d.Router, d.Protect, d.Identify)
	return svc
}

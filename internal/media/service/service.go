// Package service holds module media's use cases: presigned uploads,
// attaching them to what uses them, and the Restate services that process
// and expire them.
package service

import (
	"context"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

// Service is module media's contract.
type Service interface {
	// CreateUpload starts an upload and returns where to PUT it.
	CreateUpload(ctx context.Context, owner uuid.UUID, contentType string, size int64) (UploadTicket, error)
	// Attach gives owner's uploads to holder, such as a listing: all of them
	// or none. Uploads holder already holds pass, so a retry succeeds.
	Attach(ctx context.Context, owner, holder uuid.UUID, ids []uuid.UUID) error
	// Square gives one of owner's uploads to holder, such as an account for
	// its avatar, waits until it is processed into a square and returns the
	// square's object key. A retry returns the same key.
	Square(ctx context.Context, owner, holder, id uuid.UUID) (string, error)
	// Photos reports each upload's processing, with URLs once ready.
	Photos(ctx context.Context, ids []uuid.UUID) ([]Photo, error)
	// URL is where a client reads the processed object key.
	URL(key string) string
}

// Deps is what New builds Media from.
type Deps struct {
	Uploads repository.Uploads
	Store   *objectstore.Store
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
}

// Media implements Service in this process.
type Media struct {
	uploads repository.Uploads
	store   *objectstore.Store
	ingress *ingress.Client
	prefix  string
	now     func() time.Time
	slots   chan struct{}
	process restatex.Caller[uuid.UUID, ProcessResult]
}

var _ Service = (*Media)(nil)

func New(d Deps) *Media {
	return &Media{
		uploads: d.Uploads, store: d.Store, ingress: d.Ingress, prefix: d.Prefix, now: d.Now,
		slots:   make(chan struct{}, processSlots),
		process: restatex.Caller[uuid.UUID, ProcessResult]{Ingress: d.Ingress, Service: d.Prefix + mediaProcess, Handler: "Run"},
	}
}

// Durable returns module media's Restate services, for Mount to bind.
func (m *Media) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{
		restate.Reflect(MediaExpire{Prefix: m.prefix, Uploads: m.uploads, Store: m.store, Now: m.now}),
		restate.Reflect(MediaProcess{Prefix: m.prefix, Uploads: m.uploads, Store: m.store, Now: m.now, Slots: m.slots}),
	}
}

func (m *Media) URL(key string) string { return m.store.PublicURL(key) }

// MediaProcessService is the name, without the prefix, of the Restate
// service that processes an attached upload: handler Run, input the upload
// ID, output ProcessResult.
const MediaProcessService = mediaProcess

// UploadTicket is the UploadTicket schema in api/schemas/media.yaml.
type UploadTicket struct {
	UploadID  uuid.UUID         `json:"upload_id"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// Photo is an upload as its user shows it.
type Photo struct {
	UploadID uuid.UUID
	Status   domain.Status
	URL      string // set when ready
	ThumbURL string // set when ready
	Failure  string // set when failed
}

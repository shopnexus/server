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

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/restatex"
)

// Service is module media's contract.
type Service interface {
	// CreateUpload starts an upload and returns where to PUT it.
	CreateUpload(ctx context.Context, owner uuid.UUID, contentType string, size int64) (UploadTicket, error)
	// Attach gives owner's uploads to holder, such as a listing: all of them
	// or none, saved under writeID. Uploads holder already holds pass, so a
	// retry succeeds.
	Attach(ctx context.Context, owner, holder uuid.UUID, ids []uuid.UUID, writeID uuid.UUID) error
	// Extend keeps owner's photos for their drafts, all or none: each is
	// owner's photo, waiting before its expiry and in the store as
	// declared, otherwise domain.ErrInvalidUpload. Each then expires 30
	// days from now; nothing brings an expiry in. The uploads stay
	// waiting, so Attach takes them.
	Extend(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) error
	// Gone returns those of ids that owner's drafts can no longer name: a
	// holder took it, it expired, or no upload has the ID.
	Gone(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error)
	// Square gives one of owner's uploads to holder, such as an account for
	// its avatar, waits until it is processed into a square and returns the
	// square's object key. A retry returns the same key.
	Square(ctx context.Context, owner, holder, id uuid.UUID) (string, error)
	// Photos gives owner's uploads to holder, such as an order's review, as
	// photos, waits until each is processed and returns their keys by upload
	// ID. A retry returns the same keys.
	Photos(ctx context.Context, owner, holder uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ProcessResult, error)
	// URL is where a client reads the processed object key.
	URL(key string) string
	// QueryUpload checks that id is a photo owner uploaded to search with:
	// theirs, waiting, before its expiry and in the store as declared.
	// Anything else is domain.ErrInvalidUpload.
	QueryUpload(ctx context.Context, owner, id uuid.UUID) (QueryUpload, error)
	// QueryImage reads such an upload as a JPEG at most 768 px on its long
	// side, upright and without EXIF. Nothing is stored or published.
	QueryImage(ctx context.Context, owner, id uuid.UUID) ([]byte, error)
	// ReadPhoto reads a processed object under media/, such as a listing's
	// photo; an absent one is objectstore.ErrNotFound.
	ReadPhoto(ctx context.Context, key string) ([]byte, error)
	// SavePhoto stores data, a photo the server made or copied for owner (a
	// frame of their video, a photo of their listing elsewhere or of one of
	// their listings here), as a
	// waiting photo upload of owner's with the ID id and the given origin.
	// The caller mints id so a retry saves the same upload. It expires in
	// 24 hours like an upload the app put. Origin app, a video type or a
	// size outside the photo limits is domain.ErrInvalidUpload.
	SavePhoto(ctx context.Context, owner, id uuid.UUID, data []byte, contentType string, origin domain.Origin) error
	// HoldVideo gives owner's video upload to holder, such as a read: owner's,
	// waiting or already held by holder, before its expiry and in the store
	// as declared, saved under writeID. Anything else is
	// domain.ErrInvalidUpload.
	HoldVideo(ctx context.Context, owner, holder, id, writeID uuid.UUID) error
	// VideoKey is the object store key of a held video, for module ai to
	// read. An unknown, unheld or consumed video is objectstore.ErrNotFound.
	VideoKey(ctx context.Context, id uuid.UUID) (string, error)
	// PresignPhoto is a URL that reads such a photo for an hour.
	PresignPhoto(ctx context.Context, id uuid.UUID) (string, error)
	// Consume deletes a held video's object once its read is over and saves
	// the upload under writeID. Anything else, or a second call, changes
	// nothing.
	Consume(ctx context.Context, id, writeID uuid.UUID) error
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
// processSlots is how many photos the process decodes at once.
const processSlots = 2

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

// UploadTicket is the UploadTicket schema in api/schemas/media.yaml.
type UploadTicket struct {
	UploadID  uuid.UUID         `json:"upload_id"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

package service

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server/internal/account/service"
	aisvc "github.com/shopnexus/server/internal/ai/service"
	"github.com/shopnexus/server/internal/assist/repository"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	discoverysvc "github.com/shopnexus/server/internal/discovery/service"
	mediasvc "github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/restatex"
	"github.com/shopnexus/server/internal/provider/chotot"
	"github.com/shopnexus/server/internal/provider/embed"
	"github.com/shopnexus/server/internal/provider/llm"
)

// startHandler is the handler each read service starts a read with.
const startHandler = "Start"

// Service is module assist's contract.
type Service interface {
	// StartVideoRead starts a read of the video, once per account and key,
	// and returns the read as it is then. transcript is what the phone
	// heard; blank is none, and the server hears the video then.
	//
	// UC-LST-01
	StartVideoRead(ctx context.Context, account, key, video uuid.UUID, transcript string) (ListingReadView, error)
	// StartPhotoRead starts a read of the photo, once per account and key,
	// and returns the read as it is then.
	//
	// UC-LST-01
	StartPhotoRead(ctx context.Context, account, key, photo uuid.UUID) (ListingReadView, error)
	// StartLinkRead parses the link and starts a read of it, once per
	// account and key, and returns the read as it is then.
	//
	// UC-LST-01 A2
	StartLinkRead(ctx context.Context, account, key uuid.UUID, link string) (ListingReadView, error)
	// GetRead returns one of account's reads of any source. Another
	// account's, an unknown one and one past its expiry are
	// domain.ErrNotFound.
	//
	// UC-LST-01
	GetRead(ctx context.Context, account, id uuid.UUID) (ListingReadView, error)
	// WatchRead calls emit with one of account's reads, as GetRead returns
	// it, then with its text each time the model has written more, then
	// with the read again once it ends. It returns after the read ends,
	// WatchFor after it starts watching, or when ctx is done or the relay
	// closes. A read GetRead would not return is domain.ErrNotFound, before
	// any emit.
	//
	// UC-LST-01
	WatchRead(ctx context.Context, account, id uuid.UUID, emit func(ReadEvent) error) error
	// PreviewLink reads the listing a pasted link names, for the card the
	// app shows before the read, and stores nothing.
	//
	// UC-LST-01 A2
	PreviewLink(ctx context.Context, account uuid.UUID, link string) (LinkPreviewView, error)
	// PriceGuide prices a draft against the listings most like it and the
	// search watches it would match, and stores nothing.
	//
	// UC-LST-01 A3
	PriceGuide(ctx context.Context, account uuid.UUID, in PriceGuideInput) (PriceGuideView, error)
	// DeleteAccount deletes a deleted account's reads and waiting
	// transcripts; a second call finds nothing left.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}

// ListingSource reads a seller's listing on another site: Chợ Tốt.
type ListingSource interface {
	Ad(ctx context.Context, listID int64) (chotot.Ad, error)
	Image(ctx context.Context, url string) ([]byte, string, error)
}

// Deps is what New builds Assist from.
type Deps struct {
	Reads repository.ListingReads
	// Source is Chợ Tốt, for link reads.
	Source   ListingSource
	Accounts accountsvc.Service
	Media    mediasvc.Service
	Catalog  catalogsvc.Service
	// Discovery gives the search watches a draft would match.
	Discovery discoverysvc.Service
	// AI cuts a video's frames and hears it, from its object store key.
	AI aisvc.Service
	// Model writes a read's title and description; Relay carries the text
	// to the seller as it is written.
	Model llm.Streamer
	Relay Relay
	// Text and Image embed with the search key, for the price sample;
	// TextMin and PhotoMin are the similarity floors of search.
	Text, Image       embed.Model
	TextMin, PhotoMin float64
	// CallTimeout bounds one call to ai, the model or an embedding.
	CallTimeout time.Duration
	Log         *slog.Logger
	// Ingress and Prefix reach this process's Restate services.
	Ingress *ingress.Client
	Prefix  string
	Now     func() time.Time
}

// Assist implements Service in this process.
type Assist struct {
	d          Deps
	startVideo restatex.Caller[StartInput, StartResult]
	startLink  restatex.Caller[StartLinkInput, StartResult]
	startPhoto restatex.Caller[StartPhotoInput, StartResult]
}

var _ Service = (*Assist)(nil)

func New(d Deps) *Assist {
	return &Assist{
		d:          d,
		startVideo: restatex.Caller[StartInput, StartResult]{Ingress: d.Ingress, Service: d.Prefix + videoRead, Handler: startHandler},
		startLink:  restatex.Caller[StartLinkInput, StartResult]{Ingress: d.Ingress, Service: d.Prefix + linkRead, Handler: startHandler},
		startPhoto: restatex.Caller[StartPhotoInput, StartResult]{Ingress: d.Ingress, Service: d.Prefix + photoRead, Handler: startHandler},
	}
}

// Durable returns module assist's Restate services, for Mount to bind.
func (a *Assist) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(VideoRead{a.d}), restate.Reflect(PhotoRead{a.d}), restate.Reflect(LinkRead{a.d})}
}

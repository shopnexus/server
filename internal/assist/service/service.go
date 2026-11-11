package service

import (
	"context"
	"io"
	"log/slog"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/assist/repository"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/restatex"
	"github.com/shopnexus/server2/internal/provider/chotot"
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/frames"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Service is module assist's contract.
type Service interface {
	// StartVideoRead runs VideoRead.Start once per account and
	// idempotencyKey and returns the read as it is then.
	StartVideoRead(ctx context.Context, account uuid.UUID, idempotencyKey string, video uuid.UUID) (VideoReadView, error)
	// GetVideoRead returns one of account's video reads. Another account's,
	// an unknown one and one past its expiry are domain.ErrNotFound.
	GetVideoRead(ctx context.Context, account, id uuid.UUID) (VideoReadView, error)
	// PreviewLink reads the listing a pasted link names, for the card the
	// app shows before the read, and stores nothing.
	PreviewLink(ctx context.Context, account uuid.UUID, link string) (LinkPreviewView, error)
	// StartLinkRead runs LinkRead.Start once per account and
	// idempotencyKey and returns the read as it is then.
	StartLinkRead(ctx context.Context, account uuid.UUID, idempotencyKey, link string) (LinkReadView, error)
	// GetLinkRead returns one of account's link reads. Another account's, an
	// unknown one and one past its expiry are domain.ErrLinkReadNotFound.
	GetLinkRead(ctx context.Context, account, id uuid.UUID) (LinkReadView, error)
	// DeleteAccount deletes a deleted account's reads of both kinds; a
	// second call finds nothing left.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}

// ListingSource reads a seller's listing on another site: Chợ Tốt.
type ListingSource interface {
	Ad(ctx context.Context, listID int64) (chotot.Ad, error)
	Image(ctx context.Context, url string) ([]byte, string, error)
}

// FrameCutter cuts a video into its clearest frames: ai's
// /v1/video/frames.
type FrameCutter interface {
	Extract(ctx context.Context, video io.Reader, size int64) (frames.Result, error)
}

// Transcriber turns a video's sound into text: the stt alias.
type Transcriber interface {
	Transcribe(ctx context.Context, audio io.Reader, size int64) (string, error)
}

// Deps is what New builds Assist from.
type Deps struct {
	VideoReads repository.VideoReads
	LinkReads  repository.LinkReads
	// Source is Chợ Tốt, for link reads.
	Source   ListingSource
	Accounts accountsvc.Service
	Media    mediasvc.Service
	Catalog  catalogsvc.Service
	Frames   FrameCutter
	STT      Transcriber
	Model    llm.Client
	// Text and Image embed with the search key, for the price sample;
	// TextMin and PhotoMin are the similarity floors of search.
	Text, Image       embed.Model
	TextMin, PhotoMin float64
	// CallTimeout bounds one call to ai, STT, the model or an embedding.
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
}

var _ Service = (*Assist)(nil)

func New(d Deps) *Assist {
	return &Assist{
		d:          d,
		startVideo: restatex.Caller[StartInput, StartResult]{Ingress: d.Ingress, Service: d.Prefix + videoRead, Handler: "Start"},
		startLink:  restatex.Caller[StartLinkInput, StartResult]{Ingress: d.Ingress, Service: d.Prefix + linkRead, Handler: "Start"},
	}
}

// Durable returns module assist's Restate services, for Mount to bind.
func (a *Assist) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(VideoRead{a.d}), restate.Reflect(LinkRead{a.d})}
}

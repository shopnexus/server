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
	"github.com/shopnexus/server2/internal/provider/embed"
	"github.com/shopnexus/server2/internal/provider/frames"
	"github.com/shopnexus/server2/internal/provider/llm"
)

// Service is module assist's contract.
type Service interface {
	// StartRead runs VideoRead.Start once per account and idempotencyKey and
	// returns the read as it is then. A repeated key gets the first read.
	StartRead(ctx context.Context, account uuid.UUID, idempotencyKey string, video uuid.UUID) (VideoReadView, error)
	// GetRead returns one of account's reads. Another account's read, an
	// unknown one and one past its expiry are domain.ErrNotFound.
	GetRead(ctx context.Context, account, id uuid.UUID) (VideoReadView, error)
	// DeleteAccount deletes a deleted account's reads; a second call finds
	// nothing left.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
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
	Reads    repository.VideoReads
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
	d     Deps
	start restatex.Caller[StartInput, StartResult]
}

var _ Service = (*Assist)(nil)

func New(d Deps) *Assist {
	return &Assist{
		d:     d,
		start: restatex.Caller[StartInput, StartResult]{Ingress: d.Ingress, Service: d.Prefix + videoRead, Handler: "Start"},
	}
}

// Durable returns module assist's Restate services, for Mount to bind.
func (a *Assist) Durable() []restate.ServiceDefinition {
	return []restate.ServiceDefinition{restate.Reflect(VideoRead{a.d})}
}

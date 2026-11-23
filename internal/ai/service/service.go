// Package service is module ai: the Restate service ai, which runs the
// models of this process, and the client every caller reaches it through.
package service

import (
	"context"
	"errors"
	"fmt"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/ai/domain"
)

// Service is module ai's contract. Video and audio stay in the object
// store: a call names the object's key, and ai reads it from there.
type Service interface {
	// Transcribe returns what was said in the video or audio at key, in
	// lower case; no speech is "". A file with no audio track is
	// domain.ErrUnreadable, one longer than AI_MAX_AUDIO_SECONDS
	// domain.ErrTooLong.
	Transcribe(ctx context.Context, key string) (string, error)
	// Frames returns at most domain.MaxFrames sharp, distinct frames of the
	// video at key. An unreadable video is domain.ErrUnreadable; one too
	// short or too long is domain.ErrTooShort or domain.ErrTooLong.
	Frames(ctx context.Context, key string) (domain.FramesOutput, error)
	// EmbedImages returns one SigLIP 2 vector per data URL image or plain
	// text, in input order. A refused input, or photo vectors off, is
	// domain.ErrRefused.
	EmbedImages(ctx context.Context, inputs []string) ([][]float32, error)
}

// Client implements Service through Restate's ingress, in every role, so
// a process with the models and one without them call ai the same way.
type Client struct {
	in      *ingress.Client
	service string
}

var _ Service = Client{}

// NewClient calls the service ai of prefix through in.
func NewClient(in *ingress.Client, prefix string) Client {
	return Client{in: in, service: prefix + domain.ServiceName}
}

func (c Client) Transcribe(ctx context.Context, key string) (string, error) {
	out, err := call[domain.TranscribeInput, domain.TranscribeOutput](ctx, c, domain.HandlerTranscribe, domain.TranscribeInput{Key: key})
	return out.Text, err
}

func (c Client) Frames(ctx context.Context, key string) (domain.FramesOutput, error) {
	out, err := call[domain.FramesInput, domain.FramesOutput](ctx, c, domain.HandlerFrames, domain.FramesInput{Key: key})
	if err != nil {
		return domain.FramesOutput{}, err
	}
	for i, f := range out.Frames {
		if len(f.JPEG) == 0 {
			return domain.FramesOutput{}, fmt.Errorf("ai gave frame %d without a JPEG", i)
		}
	}
	return out, nil
}

func (c Client) EmbedImages(ctx context.Context, inputs []string) ([][]float32, error) {
	out, err := call[domain.EmbedImagesInput, domain.EmbedImagesOutput](ctx, c, domain.HandlerEmbedImages,
		domain.EmbedImagesInput{Inputs: inputs})
	if err != nil {
		return nil, err
	}
	if len(out.Vectors) != len(inputs) {
		return nil, fmt.Errorf("ai gave %d vectors for %d inputs", len(out.Vectors), len(inputs))
	}
	return out.Vectors, nil
}

// call turns a terminal error of service ai back into its domain error. Any
// other error, such as ai not registered yet or the context's end, comes
// back as it is, for the caller to retry.
func call[I, O any](ctx context.Context, c Client, handler string, in I) (O, error) {
	out, err := ingress.Service[I, O](c.in, c.service, handler).Request(ctx, in)
	if err == nil {
		return out, nil
	}
	if ie, ok := errors.AsType[ingress.Error](err); ok && restate.IsTerminalError(err) {
		if known := domain.FromCode(uint16(ie.Code())); known != nil {
			// The message is what ai logged; the sentinel is what callers
			// match.
			return out, fmt.Errorf("call %s/%s: %w", c.service, handler, known)
		}
	}
	return out, fmt.Errorf("call %s/%s: %w", c.service, handler, err)
}

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/ai/domain"
	"github.com/shopnexus/server2/internal/platform/objectstore"
)

// Speech hears a file: the Gipformer transcriber. A file with no audio
// track is domain.ErrUnreadable, one too long domain.ErrTooLong.
type Speech interface {
	Transcribe(ctx context.Context, path string) (string, error)
}

// Framer cuts a video file into its sharp, distinct frames, with ffmpeg.
// An unreadable video is domain.ErrUnreadable; one too short or too long is
// domain.ErrTooShort or domain.ErrTooLong.
type Framer interface {
	Extract(ctx context.Context, path string) (domain.FramesOutput, error)
}

// Images embeds decoded images with SigLIP 2. An image it cannot decode is
// domain.ErrRefused.
type Images interface {
	EmbedImages(ctx context.Context, images [][]byte) ([][]float32, error)
}

// Objects reads the object store; a missing key is objectstore.ErrNotFound.
type Objects interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// Deps is what NewAI builds service ai from.
type Deps struct {
	Speech Speech
	Frames Framer
	// Images is nil when AI_EMBED_MODELS is empty: photo vectors are off.
	Images  Images
	Objects Objects
	// MaxInputs is AI_EMBED_MAX_INPUTS, MaxImageBytes AI_EMBED_MAX_IMAGE_MB
	// in bytes, FrameJobs AI_FRAMES_MAX_CONCURRENT.
	MaxInputs, MaxImageBytes, FrameJobs int
	Log                                 *slog.Logger
	Prefix                              string
}

// AI is the Restate service ai. Its handlers journal nothing: a retry runs
// the model again. Every error ends the call as a terminal error whose code
// names its domain error, so Restate does not retry a refused file, and
// the caller, which retried the HTTP API it replaces, retries a failure.
type AI struct {
	d          Deps
	frameSlots chan struct{}
}

// NewAI builds service ai over the loaded models.
func NewAI(d Deps) *AI {
	return &AI{d: d, frameSlots: make(chan struct{}, d.FrameJobs)}
}

// ServiceName is ai with the prefix.
func (a *AI) ServiceName() string { return a.d.Prefix + domain.ServiceName }

// imageTypes are the data URL prefixes EmbedImages takes.
var imageTypes = []string{"data:image/jpeg;base64,", "data:image/png;base64,", "data:image/webp;base64,"}

// Transcribe hears the video or audio at the key.
func (a *AI) Transcribe(ctx restate.Context, in domain.TranscribeInput) (domain.TranscribeOutput, error) {
	start := time.Now()
	text, err := withFile(ctx, a.d.Objects, in.Key, func(path string) (string, error) {
		return a.d.Speech.Transcribe(ctx, path)
	})
	// The text stays out of the log: it is what a person said.
	a.log(ctx, domain.HandlerTranscribe, start, err, "chars", len(text))
	if err != nil {
		return domain.TranscribeOutput{}, terminal(err)
	}
	return domain.TranscribeOutput{Text: text}, nil
}

// Frames cuts the video at the key, one of at most FrameJobs at once.
func (a *AI) Frames(ctx restate.Context, in domain.FramesInput) (domain.FramesOutput, error) {
	start := time.Now()
	select {
	case a.frameSlots <- struct{}{}:
	case <-ctx.Done():
		return domain.FramesOutput{}, fmt.Errorf("wait for a frames slot: %w", ctx.Err())
	}
	out, err := withFile(ctx, a.d.Objects, in.Key, func(path string) (domain.FramesOutput, error) {
		return a.d.Frames.Extract(ctx, path)
	})
	<-a.frameSlots
	a.log(ctx, domain.HandlerFrames, start, err, "duration_s", out.Duration, "kept", len(out.Frames))
	if err != nil {
		return domain.FramesOutput{}, terminal(err)
	}
	out.Frames = out.Frames[:min(len(out.Frames), domain.MaxFrames)]
	return out, nil
}

// EmbedImages embeds data URL images, at most MaxInputs of at most
// MaxImageBytes each.
func (a *AI) EmbedImages(ctx restate.Context, in domain.EmbedImagesInput) (domain.EmbedImagesOutput, error) {
	start := time.Now()
	vs, err := a.decodeAndEmbed(ctx, in.Inputs)
	a.log(ctx, domain.HandlerEmbedImages, start, err, "images", len(in.Inputs))
	if err != nil {
		return domain.EmbedImagesOutput{}, terminal(err)
	}
	return domain.EmbedImagesOutput{Vectors: vs}, nil
}

func (a *AI) decodeAndEmbed(ctx context.Context, inputs []string) ([][]float32, error) {
	switch {
	case a.d.Images == nil:
		return nil, fmt.Errorf("%w: photo vectors are off (AI_EMBED_MODELS is empty)", domain.ErrRefused)
	case len(inputs) == 0:
		return nil, fmt.Errorf("%w: no input", domain.ErrRefused)
	case len(inputs) > a.d.MaxInputs:
		return nil, fmt.Errorf("%w: send at most %d inputs", domain.ErrRefused, a.d.MaxInputs)
	}
	images := make([][]byte, len(inputs))
	for i, s := range inputs {
		payload, ok := "", false
		for _, p := range imageTypes {
			if payload, ok = strings.CutPrefix(s, p); ok {
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("%w: input %d: images must be JPEG, PNG or WebP data URLs in base64", domain.ErrRefused, i)
		}
		if base64.StdEncoding.DecodedLen(len(payload)) > a.d.MaxImageBytes+2 {
			return nil, fmt.Errorf("%w: input %d: the image is larger than %d bytes", domain.ErrRefused, i, a.d.MaxImageBytes)
		}
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, fmt.Errorf("%w: input %d: the base64 does not decode", domain.ErrRefused, i)
		}
		if len(b) > a.d.MaxImageBytes {
			return nil, fmt.Errorf("%w: input %d: the image is larger than %d bytes", domain.ErrRefused, i, a.d.MaxImageBytes)
		}
		images[i] = b
	}
	vs, err := a.d.Images.EmbedImages(ctx, images)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(vs) != len(images) {
		return nil, fmt.Errorf("%w: %d vectors for %d images", domain.ErrFailed, len(vs), len(images))
	}
	return vs, nil
}

// log writes one line per call, with no word or picture of the person's.
func (a *AI) log(ctx context.Context, handler string, start time.Time, err error, attrs ...any) {
	level := slog.LevelInfo
	if err != nil && domain.Code(err) >= 500 {
		level = slog.LevelError
	}
	a.d.Log.Log(ctx, level, "ai call", append(attrs, "handler", handler, "code", code(err), "err", err,
		"ms", time.Since(start).Milliseconds())...)
}

func code(err error) uint16 {
	if err == nil {
		return 200
	}
	return domain.Code(err)
}

// terminal ends the call with err under its domain error's code; an error
// that is none of them is domain.ErrFailed.
func terminal(err error) error {
	if domain.Code(err) == domain.Code(domain.ErrFailed) && !errors.Is(err, domain.ErrFailed) {
		err = fmt.Errorf("%w: %w", domain.ErrFailed, err)
	}
	return restate.ToTerminalError(err, restate.WithErrorCode(restate.Code(domain.Code(err)))) //nolint:wrapcheck // err is wrapped already
}

// withFile copies the object at key to a temp file, runs f on its path and
// removes it: ffmpeg needs a seekable file for an mp4 whose index is at the
// end.
func withFile[T any](ctx context.Context, objects Objects, key string, f func(path string) (T, error)) (T, error) {
	var zero T
	if key == "" {
		return zero, fmt.Errorf("%w: no key", domain.ErrRefused)
	}
	r, err := objects.Get(ctx, key)
	if errors.Is(err, objectstore.ErrNotFound) {
		return zero, fmt.Errorf("%w: %s", domain.ErrNotFound, key)
	}
	if err != nil {
		return zero, fmt.Errorf("%w: read %s: %w", domain.ErrFailed, key, err)
	}
	defer func() { _ = r.Close() }()
	tmp, err := os.CreateTemp("", "ai-*")
	if err != nil {
		return zero, fmt.Errorf("%w: create a temp file: %w", domain.ErrFailed, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = io.Copy(tmp, r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return zero, fmt.Errorf("%w: copy %s: %w", domain.ErrFailed, key, err)
	}
	return f(tmp.Name())
}

// Package ai runs the self-hosted models: speech to text with Gipformer,
// photo vectors with SigLIP 2, and frames cut from videos with ffmpeg. A
// process with SERVER_ROLE all or ai loads them and serves the Restate
// service ai; every caller, in every role, reaches it through Restate's
// ingress with the client Client returns.
package ai

import (
	"context"
	"errors"
	"fmt"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server/internal/ai/domain"
	"github.com/shopnexus/server/internal/ai/embed"
	"github.com/shopnexus/server/internal/ai/frames"
	"github.com/shopnexus/server/internal/ai/service"
	"github.com/shopnexus/server/internal/ai/speech"
	"github.com/shopnexus/server/internal/platform/config"
	"github.com/shopnexus/server/internal/platform/module"
)

// Engines are the loaded models. Images is nil when photo vectors are off.
// Tests build their own in place of Load's.
type Engines struct {
	Speech service.Speech
	Frames service.Framer
	Images service.Images
	// Close releases the models; nil when there is nothing to release.
	Close func()
}

// Load loads the models c names: Gipformer and Silero VAD, and SigLIP 2
// when AI_EMBED_MODELS names it. It fails when a file is missing.
func Load(c config.AIConfig) (Engines, error) {
	t, err := speech.New(speech.Config{
		Models:   map[string]string{c.STTModel: c.STTDir},
		VADModel: c.VADModel, NumThreads: c.NumThreads, MaxConcurrent: c.MaxConcurrent, MaxAudioSeconds: c.MaxAudioSeconds,
	})
	if err != nil {
		return Engines{}, fmt.Errorf("load speech model: %w", err)
	}
	e := Engines{
		Speech: speechEngine{t: t, model: c.STTModel},
		Frames: frameEngine{c: frames.Config{
			MinSeconds: frames.MinSeconds, MaxSeconds: float64(c.FramesMaxSeconds), MinSharpness: c.FramesMinSharpness,
		}},
		Close: t.Close,
	}
	if c.EmbedOn() {
		em, embedErr := embed.New(embed.Config{
			Models: map[string]string{c.EmbedModel: c.EmbedDir}, NumThreads: c.NumThreads, MaxConcurrent: c.EmbedMaxConcurrent,
		})
		if embedErr != nil {
			t.Close()
			return Engines{}, fmt.Errorf("load embedding model: %w", embedErr)
		}
		e.Images = imageEngine{e: em, model: c.EmbedModel}
		e.Close = func() { em.Close(); t.Close() }
	}
	return e, nil
}

// Settings is what ai's Mount takes beyond module.Deps.
type Settings struct {
	Engines Engines
	Objects service.Objects
	Config  config.AIConfig
}

// Client is how other modules call ai, in every role.
func Client(d module.Deps) service.Service { return service.NewClient(d.Ingress, d.ServicePrefix) }

// Mount returns the Restate service ai over the loaded engines, and the
// client. It registers no route: ai has no HTTP API.
func Mount(d module.Deps, s Settings) module.Mounted[service.Service] {
	svc := service.NewAI(service.Deps{
		Speech: s.Engines.Speech, Frames: s.Engines.Frames, Images: s.Engines.Images, Objects: s.Objects,
		MaxInputs: s.Config.EmbedMaxInputs, MaxImageBytes: s.Config.EmbedMaxImageMB << 20, FrameJobs: s.Config.FramesMaxConcurrent,
		Log: d.Log, Prefix: d.ServicePrefix,
	})
	return module.Mounted[service.Service]{Services: []restate.ServiceDefinition{restate.Reflect(svc)}, Service: Client(d)}
}

type speechEngine struct {
	t     *speech.Transcriber
	model string
}

func (s speechEngine) Transcribe(ctx context.Context, path string) (string, error) {
	res, err := s.t.Transcribe(ctx, s.model, path)
	switch {
	case errors.Is(err, speech.ErrUnsupportedMedia):
		return "", fmt.Errorf("%w: %w", domain.ErrUnreadable, err)
	case errors.Is(err, speech.ErrTooLong):
		return "", fmt.Errorf("%w: %w", domain.ErrTooLong, err)
	case err != nil:
		return "", fmt.Errorf("transcribe: %w", err)
	}
	return res.Text, nil
}

type frameEngine struct{ c frames.Config }

func (f frameEngine) Extract(ctx context.Context, path string) (domain.FramesOutput, error) {
	res, err := frames.Extract(ctx, path, f.c)
	switch {
	case errors.Is(err, frames.ErrNoVideo):
		return domain.FramesOutput{Duration: res.Duration}, fmt.Errorf("%w: %w", domain.ErrUnreadable, err)
	case errors.Is(err, frames.ErrTooShort):
		return domain.FramesOutput{Duration: res.Duration}, fmt.Errorf("%w: %w", domain.ErrTooShort, err)
	case errors.Is(err, frames.ErrTooLong):
		return domain.FramesOutput{Duration: res.Duration}, fmt.Errorf("%w: %w", domain.ErrTooLong, err)
	case err != nil:
		return domain.FramesOutput{Duration: res.Duration}, fmt.Errorf("cut frames: %w", err)
	}
	out := domain.FramesOutput{Duration: res.Duration, Frames: make([]domain.Frame, len(res.Frames))}
	for i, fr := range res.Frames {
		out.Frames[i] = domain.Frame{AtSeconds: fr.AtSeconds, Sharpness: fr.Sharpness, JPEG: fr.JPEG}
	}
	return out, nil
}

type imageEngine struct {
	e     *embed.Embedder
	model string
}

func (m imageEngine) EmbedImages(ctx context.Context, images [][]byte) ([][]float32, error) {
	in := make([]embed.Input, len(images))
	for i, b := range images {
		in[i] = embed.Input{Image: b}
	}
	res, err := m.e.Embed(ctx, m.model, in)
	if errors.Is(err, embed.ErrBadImage) {
		return nil, fmt.Errorf("%w: %w", domain.ErrRefused, err)
	}
	if err != nil {
		return nil, fmt.Errorf("embed images: %w", err)
	}
	return res.Vectors, nil
}

func (m imageEngine) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	in := make([]embed.Input, len(texts))
	for i, t := range texts {
		in[i] = embed.Input{Text: t}
	}
	res, err := m.e.Embed(ctx, m.model, in)
	if err != nil {
		return nil, fmt.Errorf("embed texts: %w", err)
	}
	return res.Vectors, nil
}

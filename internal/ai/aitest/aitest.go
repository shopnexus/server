// Package aitest finds the model files and clips module ai's model tests
// read, and fakes the models for every other test. A model test whose files
// are missing skips, so go test ./... passes on a machine without them.
package aitest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shopnexus/server/internal/ai/domain"
	"github.com/shopnexus/server/internal/ai/service"
)

// ModelsDir is $AI_TEST_MODELS, or /models when it is unset: the folder
// scripts/fetch-models.sh fills. It skips t when sub, a path under that
// folder, is missing.
func ModelsDir(t *testing.T, sub string) string {
	t.Helper()
	dir := os.Getenv("AI_TEST_MODELS")
	if dir == "" {
		dir = "/models"
	}
	return need(t, "AI_TEST_MODELS", dir, sub)
}

// WavsDir is $AI_TEST_WAVS, the speech clips; it skips t when they are
// missing.
func WavsDir(t *testing.T) string {
	t.Helper()
	return need(t, "AI_TEST_WAVS", os.Getenv("AI_TEST_WAVS"), "0.wav")
}

func need(t *testing.T, env, dir, sub string) string {
	t.Helper()
	if dir == "" {
		t.Skipf("%s is not set", env)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("%s=%s must be an absolute path", env, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, sub)); err != nil { //nolint:gosec // a test's own folder
		t.Skipf("no %s under %s (%s)", sub, dir, env)
	}
	return dir
}

// Fake is module ai's engines for tests, without models: each method reads
// the file or images it is given and answers with its func. A nil func
// answers "", no frames or one zero vector of dims per image; a nil
// EmbedText refuses every text.
type Fake struct {
	Hear      func(file []byte) (string, error)
	Cut       func(file []byte) (domain.FramesOutput, error)
	Embed     func(images [][]byte) ([][]float32, error)
	EmbedText func(texts []string) ([][]float32, error)
	Dims      int
}

var (
	_ service.Speech = Fake{}
	_ service.Framer = Fake{}
	_ service.Images = Fake{}
)

func (f Fake) Transcribe(_ context.Context, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read the file: %w", err)
	}
	if f.Hear == nil {
		return "", nil
	}
	return f.Hear(b)
}

func (f Fake) Extract(_ context.Context, path string) (domain.FramesOutput, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return domain.FramesOutput{}, fmt.Errorf("read the file: %w", err)
	}
	if f.Cut == nil {
		return domain.FramesOutput{}, nil
	}
	return f.Cut(b)
}

// EmbedImages answers the images with Embed and the texts with EmbedText,
// each in one call, images first, and puts the vectors back in input order.
func (f Fake) EmbedImages(_ context.Context, inputs []domain.EmbedItem) ([][]float32, error) {
	var images [][]byte
	var texts []string
	var imageAt, textAt []int
	for i, in := range inputs {
		if in.Image != nil {
			images, imageAt = append(images, in.Image), append(imageAt, i)
		} else {
			texts, textAt = append(texts, in.Text), append(textAt, i)
		}
	}
	out := make([][]float32, len(inputs))
	if len(images) > 0 {
		vs, err := f.images(images)
		if err != nil {
			return nil, err
		}
		if err = place(out, imageAt, vs); err != nil {
			return nil, err
		}
	}
	if len(texts) > 0 {
		if f.EmbedText == nil {
			return nil, fmt.Errorf("%w: the fake embeds no text", domain.ErrRefused)
		}
		vs, err := f.EmbedText(texts)
		if err != nil {
			return nil, err
		}
		if err = place(out, textAt, vs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (f Fake) images(images [][]byte) ([][]float32, error) {
	if f.Embed != nil {
		return f.Embed(images)
	}
	vs := make([][]float32, len(images))
	for i := range vs {
		vs[i] = make([]float32, f.Dims)
	}
	return vs, nil
}

// place puts one func's vectors at their inputs' places.
func place(out [][]float32, at []int, vs [][]float32) error {
	if len(vs) != len(at) {
		return fmt.Errorf("%w: %d vectors for %d inputs", domain.ErrFailed, len(vs), len(at))
	}
	for i, v := range vs {
		out[at[i]] = v
	}
	return nil
}

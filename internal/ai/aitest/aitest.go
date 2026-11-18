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

	"github.com/shopnexus/server2/internal/ai/domain"
	"github.com/shopnexus/server2/internal/ai/service"
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
// answers "", no frames or one zero vector of dims per image.
type Fake struct {
	Hear  func(file []byte) (string, error)
	Cut   func(file []byte) (domain.FramesOutput, error)
	Embed func(images [][]byte) ([][]float32, error)
	Dims  int
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

func (f Fake) EmbedImages(_ context.Context, images [][]byte) ([][]float32, error) {
	if f.Embed != nil {
		return f.Embed(images)
	}
	vs := make([][]float32, len(images))
	for i := range vs {
		vs[i] = make([]float32, f.Dims)
	}
	return vs, nil
}

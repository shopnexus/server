//go:build measure

// Prints how real videos score, to set AI_FRAMES_MIN_SHARPNESS, and writes
// every decoded frame named by its time and sharpness. Run from the repo
// root with
//
//	AI_TEST_VIDEOS=$PWD/tmp/videos AI_TEST_FRAMES_OUT=$PWD/tmp/frames go test -tags measure -run TestMeasureSharpness -v ./frames/
package frames

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMeasureSharpness(t *testing.T) {
	videos, out := os.Getenv("AI_TEST_VIDEOS"), os.Getenv("AI_TEST_FRAMES_OUT")
	if !filepath.IsAbs(videos) || !filepath.IsAbs(out) {
		t.Fatal("set AI_TEST_VIDEOS and AI_TEST_FRAMES_OUT to absolute paths")
	}
	entries, err := os.ReadDir(videos)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dir := filepath.Join(out, e.Name())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		err := decode(context.Background(), filepath.Join(videos, e.Name()), 600, func(c *candidate) {
			t.Logf("%s %6.1f s sharpness %8.1f hash %016x", e.Name(), c.at, c.sharpness, c.hash)
			b, err := encodeJPEG(c)
			if err != nil {
				t.Error(err)
				return
			}
			name := fmt.Sprintf("%06.1f_%08.1f.jpg", c.at, c.sharpness)
			if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
				t.Error(err)
			}
		})
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
	}
}

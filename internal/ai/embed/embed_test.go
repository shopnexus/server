package embed

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()
	v := []float32{3, 4}
	normalize(v)
	if math.Abs(float64(v[0])-0.6) > 1e-6 || math.Abs(float64(v[1])-0.8) > 1e-6 {
		t.Errorf("v = %v", v)
	}
	zero := []float32{0, 0}
	normalize(zero) // must not divide by zero
	if zero[0] != 0 || zero[1] != 0 {
		t.Errorf("zero = %v", zero)
	}
}

func TestInputErrorUnwraps(t *testing.T) {
	t.Parallel()
	err := error(&InputError{Index: 2, Err: ErrBadImage})
	if !errors.Is(err, ErrBadImage) {
		t.Error("InputError does not unwrap to ErrBadImage")
	}
	if got := err.Error(); got != "input 2: bad image" {
		t.Errorf("Error() = %q", got)
	}
}

func TestNewRefusesAMissingFolder(t *testing.T) {
	t.Parallel()
	_, err := New(Config{Dir: t.TempDir(), NumThreads: 1, MaxConcurrent: 1})
	if err == nil {
		t.Fatal("New loaded a folder with no model")
	}
}

// A request waiting for a slot decodes nothing: with the only slot taken and
// the caller gone, a bad image is never read, and the answer is the
// context's error.
func TestEmbedDecodesUnderTheSlot(t *testing.T) {
	t.Parallel()
	e := &Embedder{m: &model{}, slots: make(chan struct{}, 1)}
	e.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.Embed(ctx, []Input{{Image: []byte("not an image")}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// A caller gone before a free slot is taken starts no run (the model here
// has no sessions, so a run would panic).
func TestEmbedLeftCallerStartsNoRun(t *testing.T) {
	t.Parallel()
	e := &Embedder{m: &model{}, slots: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 50 {
		if _, err := e.Embed(ctx, []Input{{Text: "a"}}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	}
}

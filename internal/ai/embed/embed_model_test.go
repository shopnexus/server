package embed

import (
	"sync"
	"testing"
)

// minCosine is the floor against transformers for the shipped variant,
// chosen by the measurement of 2026-11-09 (see README).
const minCosine = 0.999

func newEmbedder(t *testing.T, concurrent int) *Embedder {
	t.Helper()
	e, err := New(Config{Dir: modelDir(t), NumThreads: 2, MaxConcurrent: concurrent})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func TestEmbedMatchesTransformers(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	ref := loadReference(t)
	imgs, txts := embedReference(t, newEmbedder(t, 1), ref)
	for i, v := range imgs {
		if len(v) != Dimensions {
			t.Fatalf("image %d has %d dimensions", i, len(v))
		}
		if c := cosine(v, ref.Images[i].Embedding); c < minCosine {
			t.Errorf("%s: cosine %.5f < %v", ref.Images[i].File, c, minCosine)
		}
		if n := cosine(v, v); n < 0.9999 || n > 1.0001 {
			t.Errorf("%s: norm² %.6f", ref.Images[i].File, n)
		}
	}
	for i, v := range txts {
		if c := cosine(v, ref.Texts[i].Embedding); c < minCosine {
			t.Errorf("%q: cosine %.5f < %v", ref.Texts[i].Text, c, minCosine)
		}
	}
	if !sameRankings(imgs, txts, ref) {
		t.Error("a text ranks the images in another order than transformers")
	}
}

func TestEmbedConcurrently(t *testing.T) { //nolint:paralleltest // loads the model; tests in parallel would each hold a copy
	ref := loadReference(t)
	e := newEmbedder(t, 2)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { embedReference(t, e, ref) })
	}
	wg.Wait()
}

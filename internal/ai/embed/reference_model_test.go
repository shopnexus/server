package embed

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// embedReference embeds the reference images and texts with e.
func embedReference(t *testing.T, e *Embedder, ref reference) (imgs, txts [][]float32) {
	t.Helper()
	in := make([]Input, 0, len(ref.Images)+len(ref.Texts))
	for _, im := range ref.Images {
		b, err := os.ReadFile(filepath.Join("testdata", im.File))
		if err != nil {
			t.Fatal(err)
		}
		in = append(in, Input{Image: b})
	}
	for _, tx := range ref.Texts {
		in = append(in, Input{Text: tx.Text})
	}
	res, err := e.Embed(context.Background(), e.Models()[0], in)
	if err != nil {
		t.Fatal(err)
	}
	return res.Vectors[:len(ref.Images)], res.Vectors[len(ref.Images):]
}

func cosine(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s // both are unit vectors
}

// sameRankings says whether, for every text, the images rank in the same
// order as with the reference vectors.
func sameRankings(imgs, txts [][]float32, ref reference) bool {
	order := func(q []float32, set [][]float32) []int {
		idx := make([]int, len(set))
		for i := range idx {
			idx[i] = i
		}
		slices.SortFunc(idx, func(a, b int) int {
			ca, cb := cosine(q, set[a]), cosine(q, set[b])
			switch {
			case ca > cb:
				return -1
			case ca < cb:
				return 1
			}
			return 0
		})
		return idx
	}
	refImgs := make([][]float32, len(ref.Images))
	for i := range ref.Images {
		refImgs[i] = ref.Images[i].Embedding
	}
	for i := range txts {
		if !slices.Equal(order(txts[i], imgs), order(ref.Texts[i].Embedding, refImgs)) {
			return false
		}
	}
	return true
}

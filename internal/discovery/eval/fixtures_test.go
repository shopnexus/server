package eval_test

import (
	"testing"

	"github.com/shopnexus/server/internal/discovery/eval"
)

// TestSampleSetAgrees keeps the sample set whole: Load checks every key,
// kind and photo file.
func TestSampleSetAgrees(t *testing.T) {
	t.Parallel()
	listings, queries, err := eval.Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) < 40 || len(queries) < 40 {
		t.Fatalf("%d listings, %d queries; the spec asks about 40 and 40 to 50", len(listings), len(queries))
	}
	kinds := map[string]int{}
	for _, q := range queries {
		kinds[q.Kind]++
	}
	if kinds["q"] == 0 || kinds["photo"] == 0 || kinds["like"] == 0 {
		t.Fatalf("every kind needs queries: %v", kinds)
	}
}

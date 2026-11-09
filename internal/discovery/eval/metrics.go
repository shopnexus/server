// Package eval measures search on a sample set of listings and queries:
// fixtures.go reads the set, metrics.go scores a page, and eval_test.go,
// under the build tag eval, runs the set against the real models through
// make search-eval. Only the tagged test talks to a model.
package eval

import (
	"math"
	"slices"
)

// RecallAt is the share of want among the first k of got.
func RecallAt(got, want []string, k int) float64 {
	if len(want) == 0 {
		return 0
	}
	top := got[:min(k, len(got))]
	n := 0
	for _, w := range want {
		if slices.Contains(top, w) {
			n++
		}
	}
	return float64(n) / float64(len(want))
}

// NDCGAt is the normalized discounted cumulative gain of the first k of
// got, where a listing of want counts 1 and any other 0.
func NDCGAt(got, want []string, k int) float64 {
	var dcg, ideal float64
	for i, g := range got[:min(k, len(got))] {
		if slices.Contains(want, g) {
			dcg += 1 / math.Log2(float64(i)+2)
		}
	}
	for i := range min(k, len(want)) {
		ideal += 1 / math.Log2(float64(i)+2)
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

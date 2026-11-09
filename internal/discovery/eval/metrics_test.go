package eval_test

import (
	"math"
	"testing"

	"github.com/shopnexus/server2/internal/discovery/eval"
)

func TestRecallAt(t *testing.T) {
	t.Parallel()
	got := []string{"a", "x", "b"}
	for _, tc := range []struct {
		want []string
		k    int
		r    float64
	}{{[]string{"a", "b"}, 10, 1}, {[]string{"a", "b"}, 1, 0.5}, {[]string{"c"}, 10, 0}, {nil, 10, 0}} {
		if r := eval.RecallAt(got, tc.want, tc.k); r != tc.r {
			t.Errorf("RecallAt(%v, %d) = %v, want %v", tc.want, tc.k, r, tc.r)
		}
	}
}

func TestNDCGAt(t *testing.T) {
	t.Parallel()
	// a at rank 1 and b at rank 3: (1 + 1/log2(4)) / (1 + 1/log2(3)).
	want := (1 + 0.5) / (1 + 1/math.Log2(3))
	if n := eval.NDCGAt([]string{"a", "x", "b"}, []string{"a", "b"}, 10); math.Abs(n-want) > 1e-9 {
		t.Fatalf("%v, want %v", n, want)
	}
	if n := eval.NDCGAt([]string{"a", "b"}, []string{"a", "b"}, 10); n != 1 {
		t.Fatalf("perfect order: %v", n)
	}
	if n := eval.NDCGAt(nil, []string{"a"}, 10); n != 0 {
		t.Fatalf("nothing found: %v", n)
	}
}

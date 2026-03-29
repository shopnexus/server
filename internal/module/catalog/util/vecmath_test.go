package catalogutil

import (
	"math"
	"testing"
)

const tolerance = 1e-5

func approxEqual(a, b, eps float32) bool {
	return float32(math.Abs(float64(a-b))) < eps
}

// ---------- CosineSim ----------

func TestCosineSim_Identical(t *testing.T) {
	v := []float32{1, 2, 3}
	sim := CosineSim(v, v)
	if !approxEqual(sim, 1.0, tolerance) {
		t.Fatalf("expected ~1.0, got %f", sim)
	}
}

func TestCosineSim_Orthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim := CosineSim(a, b)
	if !approxEqual(sim, 0.0, tolerance) {
		t.Fatalf("expected ~0.0, got %f", sim)
	}
}

func TestCosineSim_Opposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	sim := CosineSim(a, b)
	if !approxEqual(sim, -1.0, tolerance) {
		t.Fatalf("expected ~-1.0, got %f", sim)
	}
}

func TestCosineSim_ZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	sim := CosineSim(a, b)
	if sim != 0.0 {
		t.Fatalf("expected 0.0, got %f", sim)
	}
}

// ---------- VectorNormalize ----------

func TestVectorNormalize(t *testing.T) {
	v := []float32{3, 4, 0}
	nv := VectorNormalize(v)
	n := VectorNorm(nv)
	if !approxEqual(n, 1.0, tolerance) {
		t.Fatalf("expected norm ~1.0, got %f", n)
	}
}

func TestVectorNormalize_ZeroVector(t *testing.T) {
	v := []float32{0, 0, 0}
	nv := VectorNormalize(v)
	// Should return the same slice (near-zero guard)
	if &nv[0] != &v[0] {
		t.Fatal("expected same slice returned for zero vector")
	}
}

// ---------- VectorAdd ----------

func TestVectorAdd(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{4, 5, 6}
	c := VectorAdd(a, b)
	expected := []float32{5, 7, 9}
	for i := range c {
		if c[i] != expected[i] {
			t.Fatalf("index %d: expected %f, got %f", i, expected[i], c[i])
		}
	}
}

// ---------- VectorSub ----------

func TestVectorSub(t *testing.T) {
	a := []float32{5, 7, 9}
	b := []float32{1, 2, 3}
	c := VectorSub(a, b)
	expected := []float32{4, 5, 6}
	for i := range c {
		if c[i] != expected[i] {
			t.Fatalf("index %d: expected %f, got %f", i, expected[i], c[i])
		}
	}
}

// ---------- VectorScale ----------

func TestVectorScale(t *testing.T) {
	v := []float32{2, 4, 6}
	s := VectorScale(v, 0.5)
	expected := []float32{1, 2, 3}
	for i := range s {
		if !approxEqual(s[i], expected[i], tolerance) {
			t.Fatalf("index %d: expected %f, got %f", i, expected[i], s[i])
		}
	}
}

package catalogbiz

import "math"

// cosineSim returns the cosine similarity between two float32 vectors.
// Returns 0 if either vector has zero norm.
func cosineSim(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// vectorNorm returns the L2 (Euclidean) norm of a float32 vector.
func vectorNorm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		f := float64(x)
		sum += f * f
	}
	return float32(math.Sqrt(sum))
}

// vectorNormalize returns a unit vector in the same direction as v.
// If v is near-zero (norm < 1e-9), it is returned as-is.
func vectorNormalize(v []float32) []float32 {
	n := vectorNorm(v)
	if n < 1e-9 {
		return v
	}
	return vectorScale(v, 1.0/n)
}

// vectorScale returns a new vector where each element of v is multiplied by s.
func vectorScale(v []float32, s float32) []float32 {
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * s
	}
	return out
}

// vectorAdd returns the element-wise sum of two float32 vectors.
func vectorAdd(a, b []float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i] + b[i]
	}
	return out
}

// vectorSub returns the element-wise difference (a - b) of two float32 vectors.
func vectorSub(a, b []float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i] - b[i]
	}
	return out
}

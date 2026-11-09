// Package embedtest is an embed.Model for tests: each input maps to a fixed
// unit vector, from its SHA-256 unless a test set one, so equal inputs give
// equal vectors and unrelated inputs are close to orthogonal.
package embedtest

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// Model counts its calls and inputs and fails while a failure is set.
type Model struct {
	dims   int
	mu     sync.Mutex
	fixed  map[string][]float32
	calls  int
	inputs []string
	fail   error
	slow   time.Duration
}

func New(dims int) *Model { return &Model{dims: dims, fixed: map[string][]float32{}} }

// Set makes input embed to v, normalized.
func (m *Model) Set(input string, v []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fixed[input] = unit(v)
}

// SetFail makes every call fail with err; nil ends it.
func (m *Model) SetFail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

// SetSlow makes each call wait d, or until its context ends, before it
// answers: a model that hangs.
func (m *Model) SetSlow(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slow = d
}

func (m *Model) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *Model) Inputs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.inputs...)
}

func (m *Model) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	m.mu.Lock()
	slow := m.slow
	m.mu.Unlock()
	if slow > 0 {
		select {
		case <-time.After(slow):
		case <-ctx.Done():
			return nil, fmt.Errorf("embedtest: %w", ctx.Err())
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.inputs = append(m.inputs, inputs...)
	if m.fail != nil {
		return nil, m.fail
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("embedtest: %w", err)
	}
	out := make([][]float32, len(inputs))
	for i, in := range inputs {
		if v, ok := m.fixed[in]; ok {
			out[i] = append([]float32{}, v...)
			continue
		}
		sum := sha256.Sum256([]byte(in))
		//nolint:gosec // a deterministic fake vector, not a secret
		r := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])))
		v := make([]float32, m.dims)
		for j := range v {
			v[j] = float32(r.NormFloat64())
		}
		out[i] = unit(v)
	}
	return out, nil
}

// Axis is the unit vector along axis i.
func Axis(dims, i int) []float32 {
	v := make([]float32, dims)
	v[i] = 1
	return v
}

// Near is the unit vector at cosine sim from Axis(dims, i), leaning toward
// axis j.
func Near(dims, i, j int, sim float64) []float32 {
	v := make([]float32, dims)
	v[i] = float32(sim)
	v[j] = float32(math.Sqrt(1 - sim*sim))
	return v
}

func unit(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	n := float32(math.Sqrt(s))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

package embedtest_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/shopnexus/server2/internal/provider/embed/embedtest"
)

func cos(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func TestFake(t *testing.T) {
	t.Parallel()
	m := embedtest.New(8)
	ctx := context.Background()
	a, _ := m.Embed(ctx, []string{"xe đạp"})
	b, _ := m.Embed(ctx, []string{"xe đạp"})
	if cos(a[0], b[0]) < 0.9999 || math.Abs(cos(a[0], a[0])-1) > 1e-5 || m.Calls() != 2 {
		t.Fatalf("not deterministic or not unit: %v %v %d", a, b, m.Calls())
	}
	m.Set("bike", embedtest.Axis(8, 3))
	v, _ := m.Embed(ctx, []string{"bike"})
	if v[0][3] != 1 {
		t.Fatal(v)
	}
	if s := cos(embedtest.Near(8, 0, 1, 0.7), embedtest.Axis(8, 0)); math.Abs(s-0.7) > 1e-5 {
		t.Fatal(s)
	}
	m.SetFail(errors.New("down"))
	if _, err := m.Embed(ctx, []string{"x"}); err == nil {
		t.Fatal("no failure")
	}
}

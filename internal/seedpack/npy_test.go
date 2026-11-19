package seedpack_test

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/shopnexus/server/internal/seedpack"
)

func TestHalf(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		f    float32
		half uint16
	}{
		{0, 0},
		{1, 0x3C00},
		{-2, 0xC000},
		{0.5, 0x3800},
		{65504, 0x7BFF},
		{1e6, 0x7C00},
		{6.103515625e-05, 0x0400},
		{5.960464477539063e-08, 0x0001},
		{0.333251953125, 0x3555},
	} {
		if got := seedpack.FloatToHalf(c.f); got != c.half {
			t.Errorf("FloatToHalf(%v) = %#04x, want %#04x", c.f, got, c.half)
		}
		if c.half != 0x7C00 {
			if got := seedpack.HalfToFloat(c.half); got != c.f {
				t.Errorf("HalfToFloat(%#04x) = %v, want %v", c.half, got, c.f)
			}
		}
	}
	// Every finite half survives a round trip.
	for h := range uint16(0x7C00) {
		if got := seedpack.FloatToHalf(seedpack.HalfToFloat(h)); got != h {
			t.Fatalf("%#04x came back %#04x", h, got)
		}
	}
	if !math.IsNaN(float64(seedpack.HalfToFloat(seedpack.FloatToHalf(float32(math.NaN()))))) {
		t.Error("NaN is lost")
	}
}

func TestMatrix(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "m.npy")
	m, err := seedpack.CreateMatrix(path, 3, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SetRow(1, []float32{1, -0.5, 0.25, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err = m.SetRow(0, []float32{1, 2}); err == nil {
		t.Error("a short row was written")
	}
	_ = m.Close()
	m, err = seedpack.OpenMatrix(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.Rows != 3 || m.Cols != 5 {
		t.Fatalf("shape %d×%d", m.Rows, m.Cols)
	}
	if row, rowErr := m.Row(0); rowErr != nil || row != nil {
		t.Errorf("row 0 = %v, %v; want no vector", row, rowErr)
	}
	row, err := m.Row(1)
	if err != nil || len(row) != 5 || row[1] != -0.5 || row[4] != 3 {
		t.Errorf("row 1 = %v, %v", row, err)
	}
}

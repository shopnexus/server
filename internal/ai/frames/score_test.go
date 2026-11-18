package frames

import "testing"

// checker is a w×h board of black and white squares of side cell.
func checker(w, h, cell int) []float64 {
	g := make([]float64, w*h)
	for y := range h {
		for x := range w {
			if (x/cell+y/cell)%2 == 0 {
				g[y*w+x] = 255
			}
		}
	}
	return g
}

// boxBlur averages each pixel with its neighbours within r.
func boxBlur(g []float64, w, h, r int) []float64 {
	out := make([]float64, len(g))
	for y := range h {
		for x := range w {
			var sum float64
			n := 0
			for dy := -r; dy <= r; dy++ {
				for dx := -r; dx <= r; dx++ {
					yy, xx := y+dy, x+dx
					if yy < 0 || yy >= h || xx < 0 || xx >= w {
						continue
					}
					sum += g[yy*w+xx]
					n++
				}
			}
			out[y*w+x] = sum / float64(n)
		}
	}
	return out
}

// ramp brightens left to right, or right to left when reversed.
func ramp(w, h int, reversed bool) []float64 {
	g := make([]float64, w*h)
	for y := range h {
		for x := range w {
			v := float64(x * 4)
			if reversed {
				v = float64((w - 1 - x) * 4)
			}
			g[y*w+x] = v
		}
	}
	return g
}

func TestSharpnessOrdersFocusBlurAndFlat(t *testing.T) {
	t.Parallel()
	sharp := checker(128, 128, 4)
	blurred := boxBlur(sharp, 128, 128, 3)
	flat := make([]float64, 128*128)
	s, b, f := sharpness(sharp, 128, 128), sharpness(blurred, 128, 128), sharpness(flat, 128, 128)
	if !(s > b && b > f) {
		t.Errorf("sharp %v, blurred %v, flat %v: want sharp > blurred > flat", s, b, f)
	}
	if f != 0 {
		t.Errorf("flat = %v, want 0", f)
	}
	if got := sharpness(make([]float64, 4), 2, 2); got != 0 {
		t.Errorf("a 2×2 image = %v, want 0", got)
	}
}

func TestDhash(t *testing.T) {
	t.Parallel()
	a := ramp(64, 64, false)
	if d := distance(dhash(a, 64, 64), dhash(a, 64, 64)); d != 0 {
		t.Errorf("same image: distance %d", d)
	}
	b := ramp(64, 64, true)
	if d := distance(dhash(a, 64, 64), dhash(b, 64, 64)); d <= dupDistance {
		t.Errorf("mirrored ramps: distance %d, want more than %d", d, dupDistance)
	}
	noisy := append([]float64(nil), a...)
	for i := 0; i < len(noisy); i += 97 {
		noisy[i] += 3
	}
	if d := distance(dhash(a, 64, 64), dhash(noisy, 64, 64)); d > dupDistance {
		t.Errorf("a little noise: distance %d, want at most %d", d, dupDistance)
	}
}

func TestShrinkAverages(t *testing.T) {
	t.Parallel()
	g := []float64{
		0, 10, 100, 200,
		20, 30, 300, 400,
	}
	got := shrink(g, 4, 2, 2, 1)
	if len(got) != 2 || got[0] != 15 || got[1] != 250 {
		t.Errorf("shrink = %v, want [15 250]", got)
	}
}

func TestGreyIsLuma(t *testing.T) {
	t.Parallel()
	got := grey([]byte{255, 0, 0, 0, 255, 0, 0, 0, 255}, nil)
	want := []float64{0.299 * 255, 0.587 * 255, 0.114 * 255}
	for i := range want {
		if d := got[i] - want[i]; d < -1e-9 || d > 1e-9 {
			t.Errorf("grey[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestScoreSize(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ w, h, tw, th int }{
		{1600, 900, 512, 288},
		{360, 640, 288, 512},
		{100, 50, 100, 50},
		{512, 512, 512, 512},
	} {
		if tw, th := scoreSize(c.w, c.h); tw != c.tw || th != c.th {
			t.Errorf("scoreSize(%d, %d) = %d, %d, want %d, %d", c.w, c.h, tw, th, c.tw, c.th)
		}
	}
}

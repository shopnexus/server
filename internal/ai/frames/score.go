package frames

import "math/bits"

// grey turns packed RGB into luma, reusing dst when it is large enough.
func grey(rgb []byte, dst []float64) []float64 {
	n := len(rgb) / 3
	if cap(dst) < n {
		dst = make([]float64, n)
	}
	dst = dst[:n]
	for i := range dst {
		p := rgb[3*i : 3*i+3]
		dst[i] = 0.299*float64(p[0]) + 0.587*float64(p[1]) + 0.114*float64(p[2])
	}
	return dst
}

// shrink averages a w×h grey image into tw×th. Each target pixel covers at
// least one source pixel, so a target larger than the source repeats pixels.
func shrink(g []float64, w, h, tw, th int) []float64 {
	out := make([]float64, tw*th)
	for ty := range th {
		y0 := ty * h / th
		y1 := max((ty+1)*h/th, y0+1)
		for tx := range tw {
			x0 := tx * w / tw
			x1 := max((tx+1)*w/tw, x0+1)
			var sum float64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					sum += g[y*w+x]
				}
			}
			out[ty*tw+tx] = sum / float64((y1-y0)*(x1-x0))
		}
	}
	return out
}

// scoreSize is the size of the copy a frame is scored on: at most scoreSide
// on its long side and never larger than the frame, so videos of different
// resolutions score on one scale.
func scoreSize(w, h int) (tw, th int) {
	if w >= h {
		tw = min(scoreSide, w)
		return tw, max(1, h*tw/w)
	}
	th = min(scoreSide, h)
	return max(1, w*th/h), th
}

// sharpness is the variance of the 4-neighbour Laplacian over the interior:
// edges in focus give large values, blur and flat surfaces small ones.
func sharpness(g []float64, w, h int) float64 {
	if w < 3 || h < 3 {
		return 0
	}
	var sum, sq float64
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			l := g[i-1] + g[i+1] + g[i-w] + g[i+w] - 4*g[i]
			sum += l
			sq += l * l
		}
	}
	n := float64((w - 2) * (h - 2))
	m := sum / n
	return sq/n - m*m
}

// dhash is the 64-bit difference hash: the image shrunk to 9×8, each bit
// set when a pixel is darker than its right neighbour.
func dhash(g []float64, w, h int) uint64 {
	s := shrink(g, w, h, 9, 8)
	var hash uint64
	for y := range 8 {
		for x := range 8 {
			hash <<= 1
			if s[y*9+x] < s[y*9+x+1] {
				hash |= 1
			}
		}
	}
	return hash
}

// distance is the number of bits two hashes differ in.
func distance(a, b uint64) int { return bits.OnesCount64(a ^ b) }

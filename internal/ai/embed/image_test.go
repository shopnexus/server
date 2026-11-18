package embed

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
	"testing"
)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// at is channel c at row y, column x of a pixels() result.
func at(p []float32, c, y, x int) float32 { return p[c*imageSide*imageSide+y*imageSide+x] }

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestPixelsNormalizesChannelsFirst(t *testing.T) {
	t.Parallel()
	p, err := pixels(encodePNG(t, solid(100, 60, color.NRGBA{255, 0, 0, 255})))
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 3*imageSide*imageSide {
		t.Fatalf("len = %d", len(p))
	}
	for _, yx := range [][2]int{{0, 0}, {200, 300}, {383, 383}} {
		r, g, b := at(p, 0, yx[0], yx[1]), at(p, 1, yx[0], yx[1]), at(p, 2, yx[0], yx[1])
		if !near(r, 1) || !near(g, -1) || !near(b, -1) {
			t.Errorf("at %v: %v %v %v, want 1 -1 -1", yx, r, g, b)
		}
	}
}

func TestPixelsKeepsRGBUnderTransparency(t *testing.T) {
	t.Parallel()
	// Alpha 0 over blue: PIL's convert("RGB") keeps blue, so must we.
	p, err := pixels(encodePNG(t, solid(64, 64, color.NRGBA{0, 0, 255, 0})))
	if err != nil {
		t.Fatal(err)
	}
	if r, b := at(p, 0, 10, 10), at(p, 2, 10, 10); !near(r, -1) || !near(b, 1) {
		t.Errorf("r, b = %v, %v, want -1, 1", r, b)
	}
}

func TestPixelsReadsJPEG(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, solid(80, 80, color.NRGBA{255, 255, 255, 255}), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	p, err := pixels(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if v := at(p, 1, 40, 40); v < 0.98 {
		t.Errorf("white green channel = %v", v)
	}
}

func TestPixelsIgnoresEXIFOrientation(t *testing.T) {
	t.Parallel()
	// A JPEG is embedded as stored; callers send upright photos. A 80x40
	// image stays wide: its left half red, right half blue, after resize.
	img := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	for y := range 40 {
		for x := range 80 {
			c := color.NRGBA{255, 0, 0, 255}
			if x >= 40 {
				c = color.NRGBA{0, 0, 255, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	p, err := pixels(encodePNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	if at(p, 0, 192, 20) < 0.9 || at(p, 2, 192, 360) < 0.9 {
		t.Errorf("left red %v, right blue %v", at(p, 0, 192, 20), at(p, 2, 192, 360))
	}
}

func TestPixelsRefuses(t *testing.T) {
	t.Parallel()
	tiny := encodePNG(t, solid(31, 200, color.NRGBA{0, 0, 0, 255}))
	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	for name, b := range map[string][]byte{"tiny": tiny, "gif": gif, "garbage": []byte("not an image")} {
		if _, err := pixels(b); !errors.Is(err, ErrBadImage) {
			t.Errorf("%s: err = %v, want ErrBadImage", name, err)
		}
	}
}

// pngHeader is a PNG signature and IHDR chunk claiming w x h, with no
// pixel data: enough for DecodeConfig, never decodable.
func pngHeader(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	chunk := make([]byte, 4+13)
	copy(chunk, "IHDR")
	binary.BigEndian.PutUint32(chunk[4:], w)
	binary.BigEndian.PutUint32(chunk[8:], h)
	chunk[12], chunk[13] = 8, 6 // 8-bit RGBA
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestPixelsRefusesTooManyPixelsBeforeDecoding(t *testing.T) {
	t.Parallel()
	if _, err := pixels(pngHeader(10000, 5000)); !errors.Is(err, ErrBadImage) || !strings.Contains(err.Error(), "pixels") {
		t.Errorf("err = %v, want ErrBadImage about pixels", err)
	}
}

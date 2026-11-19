package imaging_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"runtime"
	"strings"
	"testing"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/imaging"
	"github.com/shopnexus/server/internal/media/mediatest"
)

func decoded(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func reddish(c color.Color) bool {
	r, _, b, _ := c.RGBA()
	return r > 0xC000 && b < 0x4000
}

// A phone stores a portrait photo as landscape pixels with Orientation 6
// (rotate 90° clockwise); the output is portrait, its red quarter top right.
func TestProcessAppliesOrientation(t *testing.T) {
	t.Parallel()
	photo, thumb, err := imaging.Process(bytes.NewReader(mediatest.WithOrientation(mediatest.JPEG(t, 800, 600), 6)), "image/jpeg", domain.ShapePhoto)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"photo": photo, "thumb": thumb} {
		img := decoded(t, b)
		r := img.Bounds()
		if r.Dx() >= r.Dy() {
			t.Fatalf("%s is %dx%d, want portrait", name, r.Dx(), r.Dy())
		}
		if !reddish(img.At(r.Max.X-5, r.Min.Y+5)) || reddish(img.At(r.Min.X+5, r.Min.Y+5)) {
			t.Errorf("%s: the red quarter is not top right", name)
		}
	}
	for o := uint16(1); o <= 8; o++ {
		if _, _, err := imaging.Process(bytes.NewReader(mediatest.WithOrientation(mediatest.JPEG(t, 80, 60), o)), "image/jpeg", domain.ShapePhoto); err != nil {
			t.Errorf("orientation %d: %v", o, err)
		}
	}
}

// A square is the middle of the upright photo, 400 px a side, with no
// thumbnail; a smaller photo is not scaled up.
func TestProcessSquare(t *testing.T) {
	t.Parallel()
	square, thumb, err := imaging.Process(bytes.NewReader(mediatest.WithOrientation(mediatest.JPEG(t, 800, 600), 6)), "image/jpeg", domain.ShapeSquare)
	if err != nil || thumb != nil {
		t.Fatalf("%v, thumbnail %d bytes", err, len(thumb))
	}
	img := decoded(t, square)
	if r := img.Bounds(); r.Dx() != 400 || r.Dy() != 400 {
		t.Fatalf("%dx%d", r.Dx(), r.Dy())
	}
	// Upright, the photo is 600×800 with its red quarter top right; the
	// middle square keeps the red in its top right quarter.
	if !reddish(img.At(395, 5)) || reddish(img.At(5, 5)) || reddish(img.At(395, 395)) {
		t.Error("the square is not the middle of the upright photo")
	}
	small, _, err := imaging.Process(bytes.NewReader(mediatest.JPEG(t, 100, 60)), "image/jpeg", domain.ShapeSquare)
	if r := decoded(t, small).Bounds(); err != nil || r.Dx() != 60 || r.Dy() != 60 {
		t.Fatalf("small: %v %v", err, r)
	}
}

// Processing a 12-megapixel phone photo allocates little beyond its decoded
// pixels, so a few run at once without exhausting memory.
func TestProcessMemory(t *testing.T) { //nolint:paralleltest // Measures the process's allocations, which other tests would add to.
	jpg := mediatest.JPEG(t, 4000, 3000)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, _, err := imaging.Process(bytes.NewReader(jpg), "image/jpeg", domain.ShapePhoto); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 80 {
		t.Fatalf("allocated %d MB", mb)
	}
}

// A 16-bit image decodes to 8 bytes a pixel, so 30 megapixels of it is
// refused though 30 megapixels at 8 bits is not.
func TestProcessRefusesDeepImages(t *testing.T) {
	t.Parallel()
	_, _, err := imaging.Process(bytes.NewReader(mediatest.PNGHeader(6000, 5000, 16)), "image/png", domain.ShapePhoto)
	if !strings.Contains(imaging.Refusal(err), "too large") {
		t.Fatalf("16-bit: %v", err)
	}
	_, _, err = imaging.Process(bytes.NewReader(mediatest.PNGHeader(6000, 5000, 8)), "image/png", domain.ShapePhoto)
	if r := imaging.Refusal(err); r == "" || strings.Contains(r, "too large") {
		t.Fatalf("8-bit header only: want a read failure, got %v", err)
	}
}

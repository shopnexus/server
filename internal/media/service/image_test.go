package service

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"runtime"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/media/domain"
)

// jpegOf encodes a w×h image, red in its top-left quarter and blue
// elsewhere.
func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{0, 0, 255, 255}
			if x < w/2 && y < h/2 {
				c = color.RGBA{255, 0, 0, 255}
			}
			m.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withOrientation puts an APP1 EXIF segment whose IFD0 holds only the
// Orientation tag right after a JPEG's SOI marker.
func withOrientation(jpg []byte, orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08") // big-endian, IFD0 at offset 8
	ifd := make([]byte, 2+12+4)
	binary.BigEndian.PutUint16(ifd[0:], 1)      // one entry
	binary.BigEndian.PutUint16(ifd[2:], 0x0112) // Orientation
	binary.BigEndian.PutUint16(ifd[4:], 3)      // SHORT
	binary.BigEndian.PutUint32(ifd[6:], 1)      // one value
	binary.BigEndian.PutUint16(ifd[10:], orientation)
	payload := append(append([]byte("Exif\x00\x00"), tiff...), ifd...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append(append(append([]byte{}, jpg[:2]...), seg...), payload...)
	return append(out, jpg[2:]...)
}

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
	photo, thumb, err := process(bytes.NewReader(withOrientation(jpegOf(t, 800, 600), 6)), "image/jpeg", domain.ShapePhoto)
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
		if _, _, err := process(bytes.NewReader(withOrientation(jpegOf(t, 80, 60), o)), "image/jpeg", domain.ShapePhoto); err != nil {
			t.Errorf("orientation %d: %v", o, err)
		}
	}
}

// A square is the middle of the upright photo, 400 px a side, with no
// thumbnail; a smaller photo is not scaled up.
func TestProcessSquare(t *testing.T) {
	t.Parallel()
	square, thumb, err := process(bytes.NewReader(withOrientation(jpegOf(t, 800, 600), 6)), "image/jpeg", domain.ShapeSquare)
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
	small, _, err := process(bytes.NewReader(jpegOf(t, 100, 60)), "image/jpeg", domain.ShapeSquare)
	if r := decoded(t, small).Bounds(); err != nil || r.Dx() != 60 || r.Dy() != 60 {
		t.Fatalf("small: %v %v", err, r)
	}
}

// Processing a 12-megapixel phone photo allocates little beyond its decoded
// pixels, so a few run at once without exhausting memory.
func TestProcessMemory(t *testing.T) { //nolint:paralleltest // Measures the process's allocations, which other tests would add to.
	jpg := jpegOf(t, 4000, 3000)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, _, err := process(bytes.NewReader(jpg), "image/jpeg", domain.ShapePhoto); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 80 {
		t.Fatalf("allocated %d MB", mb)
	}
}

// pngHeaderOf is a PNG signature and an IHDR chunk declaring w×h with depth
// bits per channel, RGBA: enough for DecodeConfig.
func pngHeaderOf(w, h uint32, depth byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = depth, 6
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

// A 16-bit image decodes to 8 bytes a pixel, so 30 megapixels of it is
// refused though 30 megapixels at 8 bits is not.
func TestProcessRefusesDeepImages(t *testing.T) {
	t.Parallel()
	_, _, err := process(bytes.NewReader(pngHeaderOf(6000, 5000, 16)), "image/png", domain.ShapePhoto)
	if !strings.Contains(refusal(err), "too large") {
		t.Fatalf("16-bit: %v", err)
	}
	_, _, err = process(bytes.NewReader(pngHeaderOf(6000, 5000, 8)), "image/png", domain.ShapePhoto)
	if r := refusal(err); r == "" || strings.Contains(r, "too large") {
		t.Fatalf("8-bit header only: want a read failure, got %v", err)
	}
}

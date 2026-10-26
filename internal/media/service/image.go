package service

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/shopnexus/server2/internal/media/domain"
)

const (
	// maxPixels refuses an image whose header declares more, before it is
	// decoded: a small file can declare a huge image and exhaust memory.
	maxPixels = 40_000_000
	// maxDecoded bounds the decoded pixels in bytes, 40 megapixels at 4
	// bytes: an image with 16 bits a channel takes 8 bytes a pixel.
	maxDecoded = 160_000_000
	// processSlots is how many photos the process decodes at once.
	processSlots = 2
	photoSide    = 1600
	thumbSide    = 400
	quality      = 85
)

// refusedError is a photo processing refuses; its message is the reason the
// seller sees.
type refusedError struct{ reason string }

func (e refusedError) Error() string { return e.reason }

// matchesType reports whether head starts with the magic bytes of
// contentType.
func matchesType(head []byte, contentType string) bool {
	switch contentType {
	case "image/jpeg":
		return bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF})
	case "image/png":
		return bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n"))
	case "image/webp":
		return len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP"))
	default:
		return false
	}
}

// process decodes an uploaded photo and encodes it again as a JPEG at most
// photoSide on its long side, turned upright by its EXIF orientation, and a
// thumbnail at most thumbSide made from that photo. Encoding from pixels
// drops every EXIF field, GPS included. A photo it refuses is a
// refusedError.
func process(r io.Reader, contentType string) (photo, thumb []byte, err error) {
	raw, err := io.ReadAll(io.LimitReader(r, domain.MaxSize+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read the upload: %w", err)
	}
	if len(raw) > domain.MaxSize {
		return nil, nil, refusedError{"the file is larger than 15 MB"}
	}
	if !matchesType(raw, contentType) {
		return nil, nil, refusedError{"the file is not a " + contentType + " image"}
	}
	// The decoder of the declared type only: a PNG uploaded as a JPEG is
	// refused, as the magic bytes already showed.
	decodeConfig, decode := jpeg.DecodeConfig, jpeg.Decode
	switch contentType {
	case "image/png":
		decodeConfig, decode = png.DecodeConfig, png.Decode
	case "image/webp":
		decodeConfig, decode = webp.DecodeConfig, webp.Decode
	default: // image/jpeg, the only other type matchesType lets through
	}
	cfg, err := decodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, refusedError{"the image cannot be read"}
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if cfg.Width <= 0 || cfg.Height <= 0 || pixels > maxPixels || pixels*bytesPerPixel(cfg.ColorModel) > maxDecoded {
		return nil, nil, refusedError{"the image is too large; at most 40 megapixels, 20 at 16 bits a channel"}
	}
	img, err := decode(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, refusedError{"the image cannot be read"}
	}
	small := orient(shrink(img, photoSide), orientation(raw, contentType))
	if photo, err = encodeJPEG(small); err != nil {
		return nil, nil, err
	}
	if thumb, err = encodeJPEG(shrink(small, thumbSide)); err != nil {
		return nil, nil, err
	}
	return photo, thumb, nil
}

// bytesPerPixel is what a decoder of model allocates for one pixel.
func bytesPerPixel(model color.Model) int64 {
	switch model {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2
	case color.YCbCrModel: // 4:4:4 at most
		return 3
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	default: // RGBA, NRGBA, CMYK, NYCbCrA, and a palette's 1 byte rounded up
		return 4
	}
}

// shrink scales img to at most side on its long side, never up. A box
// filter by the largest whole factor that keeps it at least side does most
// of the work, and bilinear the rest; neither keeps more than its output.
func shrink(img image.Image, side int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > side {
		if k := long / side; k >= 2 {
			img = box(img, k)
		}
		w, h = max(1, w*side/long), max(1, h*side/long)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst
}

// box averages each k×k block of img into one pixel.
func box(img image.Image, k int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx()/k, b.Dy()/k
	src, ok := img.(image.RGBA64Image)
	at := func(x, y int) color.RGBA64 {
		if ok {
			return src.RGBA64At(x, y)
		}
		r, g, bl, a := img.At(x, y).RGBA()
		return color.RGBA64{uint16(r), uint16(g), uint16(bl), uint16(a)} //nolint:gosec // RGBA returns 16-bit values.
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint64(k * k) //nolint:gosec // k is at least 2.
	for oy := range h {
		for ox := range w {
			var r, g, bl, a uint64
			for y := b.Min.Y + oy*k; y < b.Min.Y+(oy+1)*k; y++ {
				for x := b.Min.X + ox*k; x < b.Min.X+(ox+1)*k; x++ {
					c := at(x, y)
					r, g, bl, a = r+uint64(c.R), g+uint64(c.G), bl+uint64(c.B), a+uint64(c.A)
				}
			}
			i := dst.PixOffset(ox, oy)
			// An average of 16-bit values, shifted to 8 bits, fits a byte.
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), uint8(a/n>>8) //nolint:gosec // See above.
		}
	}
	return dst
}

// orientation reads the EXIF Orientation tag (1 to 8) of a JPEG, or 1 when
// it has none. Only JPEG is read: the app converts HEIC to JPEG, and phones
// write PNG and WebP upright.
func orientation(raw []byte, contentType string) int {
	if contentType != "image/jpeg" {
		return 1
	}
	for i := 2; i+4 <= len(raw) && raw[i] == 0xFF; {
		marker, size := raw[i+1], int(binary.BigEndian.Uint16(raw[i+2:]))
		if marker == 0xDA || size < 2 || i+2+size > len(raw) { // start of scan: no more metadata
			return 1
		}
		seg := raw[i+4 : i+2+size]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return exifOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

// exifOrientation reads tag 0x0112 from IFD0 of a TIFF block.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 1
	}
	for e := range int(order.Uint16(tiff[ifd:])) {
		at := ifd + 2 + e*12
		if at+12 > len(tiff) {
			return 1
		}
		if order.Uint16(tiff[at:]) == 0x0112 {
			if o := int(order.Uint16(tiff[at+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}

// orient turns img upright for an EXIF orientation: 2 mirrors it, 3 turns
// it 180°, 4 flips it, 5 transposes it, 6 turns it 90° clockwise, 7
// transverses it, 8 turns it 90° counterclockwise.
func orient(img *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			default: // 8
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dst.PixOffset(dx, dy):][:4], img.Pix[img.PixOffset(x, y):][:4])
		}
	}
	return dst
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// refusal returns the reason of a refused photo, or "" for any other error.
func refusal(err error) string {
	if r, ok := errors.AsType[refusedError](err); ok {
		return r.reason
	}
	return ""
}

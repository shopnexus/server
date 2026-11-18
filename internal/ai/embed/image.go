package embed

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg" // registers JPEG
	_ "image/png"  // registers PNG

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers WebP
)

const (
	// imageSide is the vision tower's input side.
	imageSide = 384
	// minSide refuses images too small to mean anything, and the 1x1
	// inputs that crashed other servers.
	minSide = 32
	// maxPixels bounds the decoded size, checked from the header first.
	maxPixels = 40_000_000
)

// ErrBadImage marks an input that is not an image the service takes.
var ErrBadImage = errors.New("bad image")

// pixels decodes b and returns the vision tower's input for one image, as
// transformers' SiglipImageProcessor builds it: RGB as stored (alpha
// dropped, as PIL's convert("RGB") does), bilinear resize to 384x384
// without keeping the aspect, then (v/255 - 0.5) / 0.5, channels first.
// EXIF orientation is not applied.
func pixels(b []byte) ([]float32, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: not a JPEG, PNG or WebP", ErrBadImage)
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return nil, fmt.Errorf("%w: %s is not JPEG, PNG or WebP", ErrBadImage, format)
	}
	if cfg.Width < minSide || cfg.Height < minSide {
		return nil, fmt.Errorf("%w: %dx%d is under %d px a side", ErrBadImage, cfg.Width, cfg.Height, minSide)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("%w: %dx%d is over %d pixels", ErrBadImage, cfg.Width, cfg.Height, maxPixels)
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadImage, err)
	}

	// Non-premultiplied, so a transparent pixel keeps its stored RGB.
	rgb := image.NewNRGBA(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			c := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA) //nolint:errcheck,revive // NRGBAModel returns color.NRGBA
			c.A = 255
			rgb.SetNRGBA(x, y, c)
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, imageSide, imageSide))
	draw.BiLinear.Scale(dst, dst.Bounds(), rgb, rgb.Bounds(), draw.Src, nil)

	out := make([]float32, 3*imageSide*imageSide)
	plane := imageSide * imageSide
	for y := range imageSide {
		for x := range imageSide {
			i := dst.PixOffset(x, y)
			for c := range 3 {
				out[c*plane+y*imageSide+x] = float32(dst.Pix[i+c])/127.5 - 1
			}
		}
	}
	return out, nil
}

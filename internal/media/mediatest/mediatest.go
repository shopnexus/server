// Package mediatest builds module media, photos and uploads for the tests of
// modules that take photos.
package mediatest

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// Service is module media on db and store, reaching its flows through in
// under prefix. A test that needs the flows serves its Durable services.
func Service(db *pgxpool.Pool, store *objectstore.Store, in *ingress.Client, prefix string) *service.Media {
	return service.New(service.Deps{Uploads: repository.NewUploads(db), Store: store, Ingress: in, Prefix: prefix, Now: time.Now})
}

// JPEG encodes a w×h image, red in its top-left quarter and blue
// elsewhere.
func JPEG(tb testing.TB, w, h int) []byte {
	tb.Helper()
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
		tb.Fatal(err)
	}
	return b.Bytes()
}

// Upload creates an upload of contentType for owner, PUTs body to its
// presigned URL and returns its ID.
func Upload(tb testing.TB, m service.Service, owner uuid.UUID, contentType string, body []byte) uuid.UUID {
	tb.Helper()
	ticket, err := m.CreateUpload(context.Background(), owner, contentType, int64(len(body)))
	if err != nil {
		tb.Fatal(err)
	}
	req, err := http.NewRequestWithContext(tb.Context(), http.MethodPut, ticket.URL, bytes.NewReader(body))
	if err != nil {
		tb.Fatal(err)
	}
	for k, v := range ticket.Headers {
		req.Header.Set(k, v)
	}
	req.ContentLength = int64(len(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		tb.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		tb.Fatalf("put: %d", res.StatusCode)
	}
	return ticket.UploadID
}

// WithOrientation puts an APP1 EXIF segment whose IFD0 holds only the
// Orientation tag right after a JPEG's SOI marker.
func WithOrientation(jpg []byte, orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08") // big-endian, IFD0 at offset 8
	ifd := make([]byte, 2+12+4)
	binary.BigEndian.PutUint16(ifd[0:], 1)      // one entry
	binary.BigEndian.PutUint16(ifd[2:], 0x0112) // Orientation
	binary.BigEndian.PutUint16(ifd[4:], 3)      // SHORT
	binary.BigEndian.PutUint32(ifd[6:], 1)      // one value
	binary.BigEndian.PutUint16(ifd[10:], orientation)
	payload := append(append([]byte("Exif\x00\x00"), tiff...), ifd...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2)) //nolint:gosec // a segment of a few dozen bytes
	out := append(append(append([]byte{}, jpg[:2]...), seg...), payload...)
	return append(out, jpg[2:]...)
}

// PNGHeader is a PNG signature and an IHDR chunk declaring w×h with depth
// bits per channel, RGBA: enough for DecodeConfig.
func PNGHeader(w, h uint32, depth byte) []byte {
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

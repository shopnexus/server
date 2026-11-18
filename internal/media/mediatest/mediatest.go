// Package mediatest builds module media, photos and uploads for the tests of
// modules that take photos.
package mediatest

import (
	"bytes"
	"context"
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

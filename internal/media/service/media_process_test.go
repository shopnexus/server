package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// photo is a w×h image with a gradient, so the encoder has work to do.
func photo(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	return m
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, photo(w, h), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// exifMarker is an APP1 segment holding EXIF.
var exifMarker = []byte{0xFF, 0xE1}

func hasExif(b []byte) bool {
	i := bytes.Index(b, exifMarker)
	return i >= 0 && bytes.HasPrefix(b[i+4:], []byte("Exif\x00\x00"))
}

// withExif puts an APP1 EXIF segment, as a phone writes with GPS, right
// after a JPEG's SOI marker.
func withExif(t *testing.T, jpg []byte) []byte {
	t.Helper()
	tiff := []byte("II*\x00\x08\x00\x00\x00\x00\x00") // little-endian header, an empty IFD
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := append([]byte{0xFF, 0xE1, 0, 0}, payload...)
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, jpg[2:]...)
	if !hasExif(out) {
		t.Fatal("the fixture has no EXIF")
	}
	return out
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, photo(w, h)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngHeader is a PNG that declares w×h and holds no pixels: decoding the
// header alone must refuse it.
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2 // 8-bit RGB
	chunk := append([]byte("IHDR"), ihdr...)
	out := append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0, 13)
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

// webpBytes is a 1×1 lossless WebP.
func webpBytes(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// uploadAs creates an attached upload of contentType holding body.
func (e env) uploadAs(t *testing.T, contentType string, body []byte) uuid.UUID {
	t.Helper()
	owner := e.account(t)
	ticket, err := e.media.CreateUpload(context.Background(), owner, contentType, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPut, ticket.URL, bytes.NewReader(body))
	for k, v := range ticket.Headers {
		req.Header.Set(k, v)
	}
	req.ContentLength = int64(len(body))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if err := e.media.Attach(context.Background(), owner, uuid.NewV7(), []uuid.UUID{ticket.UploadID}); err != nil {
		t.Fatal(err)
	}
	return ticket.UploadID
}

func (e env) process(t *testing.T, id uuid.UUID) service.ProcessResult {
	t.Helper()
	res, err := ingress.Service[uuid.UUID, service.ProcessResult](e.ingress, e.prefix+"MediaProcess", "Run").
		Request(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e env) read(t *testing.T, key string) []byte {
	t.Helper()
	r, err := e.store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func longSide(t *testing.T, b []byte) int {
	t.Helper()
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || format != "jpeg" {
		t.Fatalf("%s %v", format, err)
	}
	return max(cfg.Width, cfg.Height)
}

func TestProcessJPEGPNGWebP(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for name, c := range map[string]struct {
		contentType string
		body        []byte
		long        int
	}{
		"jpeg": {"image/jpeg", jpegBytes(t, 2400, 1800), 1600},
		"png":  {"image/png", pngBytes(t, 900, 1200), 1200},
		"webp": {"image/webp", webpBytes(t), 1},
	} {
		id := e.uploadAs(t, c.contentType, c.body)
		if res := e.process(t, id); res.Status != domain.StatusReady {
			t.Fatalf("%s: %+v", name, res)
		}
		u, err := repository.NewUploads(e.db).Get(context.Background(), id)
		if err != nil || u.Status() != domain.StatusReady {
			t.Fatalf("%s: %s %v", name, u.Status(), err)
		}
		if got := longSide(t, e.read(t, u.PhotoKey())); got != c.long {
			t.Errorf("%s: photo long side %d, want %d", name, got, c.long)
		}
		if got := longSide(t, e.read(t, u.ThumbKey())); got > 400 {
			t.Errorf("%s: thumbnail long side %d", name, got)
		}
		if _, _, err := e.store.Stat(context.Background(), u.Key()); err == nil || !strings.Contains(err.Error(), objectstore.ErrNotFound.Error()) {
			t.Errorf("%s: the original is still there: %v", name, err)
		}
	}
}

func TestProcessDropsExif(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.uploadAs(t, "image/jpeg", withExif(t, jpegBytes(t, 800, 600)))
	if res := e.process(t, id); res.Status != domain.StatusReady {
		t.Fatalf("%+v", res)
	}
	u, _ := repository.NewUploads(e.db).Get(context.Background(), id)
	for _, key := range []string{u.PhotoKey(), u.ThumbKey()} {
		if hasExif(e.read(t, key)) {
			t.Fatalf("%s keeps EXIF", key)
		}
	}
}

func TestProcessRefusesHugeImages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.uploadAs(t, "image/png", pngHeader(10000, 10000))
	res := e.process(t, id)
	if res.Status != domain.StatusFailed || !strings.Contains(res.Failure, "too large") {
		t.Fatalf("%+v", res)
	}
}

func TestProcessRefusesWrongContent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for name, c := range map[string]struct {
		contentType string
		body        []byte
	}{
		"text as jpeg": {"image/jpeg", []byte("hello, not a photo")},
		"png as jpeg":  {"image/jpeg", pngBytes(t, 10, 10)},
	} {
		res := e.process(t, e.uploadAs(t, c.contentType, c.body))
		if res.Status != domain.StatusFailed || res.Failure == "" {
			t.Errorf("%s: %+v", name, res)
		}
	}
}

// Processing an upload again returns what the first run decided.
func TestProcessTwiceIsTheSame(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.uploadAs(t, "image/jpeg", jpegBytes(t, 100, 100))
	first, second := e.process(t, id), e.process(t, id)
	if first.Status != domain.StatusReady || second != first {
		t.Fatalf("%+v %+v", first, second)
	}
}

// A photo waits while every processing slot is taken, and is processed once
// one frees.
func TestProcessWaitsForASlot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.uploadAs(t, "image/jpeg", jpegBytes(t, 64, 48))
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	p := restatetest.Prefix()
	in := restatetest.Start(t, restate.Reflect(service.MediaProcess{
		Prefix: p, Uploads: repository.NewUploads(e.db), Store: e.store, Now: time.Now, Slots: slots,
	}))
	done := make(chan service.ProcessResult, 1)
	go func() {
		res, _ := ingress.Service[uuid.UUID, service.ProcessResult](in, p+"MediaProcess", "Run").
			Request(context.Background(), id)
		done <- res
	}()
	select {
	case res := <-done:
		t.Fatalf("processed while every slot was taken: %+v", res)
	case <-time.After(time.Second):
	}
	<-slots
	select {
	case res := <-done:
		if res.Status != domain.StatusReady {
			t.Fatalf("%+v", res)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("not processed after the slot freed")
	}
}

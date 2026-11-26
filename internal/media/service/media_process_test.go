package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

// exifMarker is an APP1 segment holding EXIF.
var exifMarker = []byte{0xFF, 0xE1}

func hasExif(b []byte) bool {
	i := bytes.Index(b, exifMarker)
	return i >= 0 && bytes.HasPrefix(b[i+4:], []byte("Exif\x00\x00"))
}

// pngBytes is a w×h PNG with a gradient, so the encoder has work to do.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
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
	owner := accounttest.New(t, e.db)
	id := mediatest.Upload(t, e.media, owner, contentType, body)
	if err := e.media.Attach(context.Background(), owner, uuid.NewV7(), []uuid.UUID{id}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	return id
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

// UC-SHR-01.
func TestProcessJPEGPNGWebP(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for name, c := range map[string]struct {
		contentType string
		body        []byte
		long        int
	}{
		"jpeg": {"image/jpeg", mediatest.JPEG(t, 2400, 1800), 1600},
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

// UC-SHR-01 · NFR-PRIV-01.
func TestProcessDropsExif(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.uploadAs(t, "image/jpeg", mediatest.WithOrientation(mediatest.JPEG(t, 800, 600), 1))
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

// UC-SHR-01 E3.
func TestProcessRefusesHugeImages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	id := e.uploadAs(t, "image/png", mediatest.PNGHeader(10000, 10000, 8))
	res := e.process(t, id)
	if res.Status != domain.StatusFailed || !strings.Contains(res.Failure, "too large") {
		t.Fatalf("%+v", res)
	}
}

// UC-SHR-01 E3.
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
	id := e.uploadAs(t, "image/jpeg", mediatest.JPEG(t, 100, 100))
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
	id := e.uploadAs(t, "image/jpeg", mediatest.JPEG(t, 64, 48))
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

// Processing says which photos the server copied from a link.
func TestProcessMarksCopies(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	copied := uuid.NewV7()
	if err := e.media.SavePhoto(ctx, owner, copied, mediatest.JPEG(t, 64, 48), "image/jpeg", domain.OriginLink); err != nil {
		t.Fatal(err)
	}
	put := e.upload(t, owner, mediatest.JPEG(t, 64, 48))
	if err := e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{copied, put}, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if res := e.process(t, copied); res.Status != domain.StatusReady || !res.Copied {
		t.Fatalf("copied: %+v", res)
	}
	if res := e.process(t, put); res.Status != domain.StatusReady || res.Copied {
		t.Fatalf("put by the app: %+v", res)
	}
	if res := e.process(t, copied); !res.Copied {
		t.Fatalf("a second run: %+v", res)
	}
}

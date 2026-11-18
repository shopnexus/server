package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"uuid"

	restate "github.com/restatedev/sdk-go"

	"github.com/shopnexus/server2/internal/ai/aitest"
	"github.com/shopnexus/server2/internal/ai/domain"
	"github.com/shopnexus/server2/internal/ai/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/provider/embed"
)

// serve binds service ai over fake engines on the test stack and returns
// its client and a function that puts a file in the object store.
func serve(t *testing.T, f aitest.Fake, photos bool) (c service.Client, put func(body string) string) {
	t.Helper()
	store := objectstoretest.New(t)
	p := restatetest.Prefix()
	d := service.Deps{
		Speech: f, Frames: f, Objects: store, MaxInputs: 2, MaxImageBytes: 8, FrameJobs: 1,
		Log: slog.New(slog.DiscardHandler), Prefix: p,
	}
	if photos {
		d.Images = f
	}
	in := restatetest.Start(t, restate.Reflect(service.NewAI(d)))
	put = func(body string) string {
		key := "ai-test/" + uuid.NewV7().String()
		if err := store.Put(context.Background(), key, "video/mp4", strings.NewReader(body), int64(len(body))); err != nil {
			t.Fatal(err)
		}
		return key
	}
	return service.NewClient(in, p), put
}

func dataURL(b []byte) string { return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b) }

func TestTranscribeAndFrames(t *testing.T) {
	t.Parallel()
	frames := make([]domain.Frame, 10)
	for i := range frames {
		frames[i] = domain.Frame{AtSeconds: float64(i), Sharpness: 1, JPEG: []byte{0xff, 0xd8}}
	}
	c, put := serve(t, aitest.Fake{
		Hear: func(file []byte) (string, error) {
			if string(file) == "silent" {
				return "", domain.ErrUnreadable
			}
			return "nghe " + string(file), nil
		},
		Cut: func(file []byte) (domain.FramesOutput, error) {
			switch string(file) {
			case "short":
				return domain.FramesOutput{}, domain.ErrTooShort
			case "long":
				return domain.FramesOutput{}, domain.ErrTooLong
			case "broken":
				return domain.FramesOutput{}, errors.New("ffmpeg crashed")
			}
			return domain.FramesOutput{Duration: 12, Frames: frames}, nil
		},
	}, false)
	ctx := context.Background()
	if text, err := c.Transcribe(ctx, put("xe đạp")); err != nil || text != "nghe xe đạp" {
		t.Fatalf("Transcribe: %q %v", text, err)
	}
	if _, err := c.Transcribe(ctx, put("silent")); !errors.Is(err, domain.ErrUnreadable) {
		t.Fatalf("no audio: %v", err)
	}
	if _, err := c.Transcribe(ctx, "ai-test/none"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
	out, err := c.Frames(ctx, put("bicycle"))
	if err != nil || out.Duration != 12 || len(out.Frames) != domain.MaxFrames || !bytes.Equal(out.Frames[7].JPEG, frames[7].JPEG) {
		t.Fatalf("Frames: %+v %v", out, err)
	}
	for body, want := range map[string]error{"short": domain.ErrTooShort, "long": domain.ErrTooLong} {
		if _, err = c.Frames(ctx, put(body)); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", body, err, want)
		}
	}
	_, err = c.Frames(ctx, put("broken"))
	for _, e := range []error{domain.ErrUnreadable, domain.ErrNotFound, domain.ErrTooShort, domain.ErrTooLong, domain.ErrRefused} {
		if err == nil || errors.Is(err, e) {
			t.Fatalf("a failed model: %v, want a plain error", err)
		}
	}
}

func TestEmbedImages(t *testing.T) {
	t.Parallel()
	c, _ := serve(t, aitest.Fake{Embed: func(images [][]byte) ([][]float32, error) {
		vs := make([][]float32, len(images))
		for i, img := range images {
			vs[i] = []float32{float32(len(img)), 0}
		}
		return vs, nil
	}}, true)
	ctx := context.Background()
	vs, err := c.EmbedImages(ctx, []string{dataURL([]byte("ab")), dataURL([]byte("abcd"))})
	if err != nil || len(vs) != 2 || vs[0][0] != 2 || vs[1][0] != 4 {
		t.Fatalf("EmbedImages: %v %v", vs, err)
	}
	for name, inputs := range map[string][]string{
		"none":      {},
		"too many":  {dataURL([]byte("a")), dataURL([]byte("b")), dataURL([]byte("c"))},
		"not image": {"xe đạp"},
		"gif":       {"data:image/gif;base64,AAAA"},
		"too big":   {dataURL([]byte("123456789"))},
		"bad b64":   {"data:image/png;base64,%%%"},
	} {
		if _, err = c.EmbedImages(ctx, inputs); !errors.Is(err, domain.ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Photos checks the dimensions and turns a refusal into embed's.
	photos := service.Photos{AI: c, Dims: 2}
	if _, err = photos.Embed(ctx, []string{"xe đạp"}); !errors.Is(err, embed.ErrRefused) {
		t.Fatalf("Photos refusal: %v", err)
	}
	if _, err = (service.Photos{AI: c, Dims: 3}).Embed(ctx, []string{dataURL([]byte("ab"))}); err == nil {
		t.Fatal("Photos took 2 dimensions for 3")
	}
}

func TestEmbedImagesOff(t *testing.T) {
	t.Parallel()
	c, _ := serve(t, aitest.Fake{}, false)
	if _, err := c.EmbedImages(context.Background(), []string{dataURL([]byte("ab"))}); !errors.Is(err, domain.ErrRefused) {
		t.Fatalf("photo vectors off: %v", err)
	}
}

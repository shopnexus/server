package service_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/media/domain"
)

// Square attaches the upload, waits for its processing and returns the key
// of a 400 px square without EXIF; asked again, it returns the same key.
func TestSquare(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, holder := e.account(t), uuid.NewV7()
	id := e.upload(t, owner, withExif(t, jpegBytes(t, 800, 600)))
	key, err := e.media.Square(ctx, owner, holder, id)
	if err != nil {
		t.Fatal(err)
	}
	b := e.read(t, key)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != 400 || cfg.Height != 400 || hasExif(b) {
		t.Fatalf("%dx%d, exif %v, %v", cfg.Width, cfg.Height, hasExif(b), err)
	}
	if again, err := e.media.Square(ctx, owner, holder, id); err != nil || again != key {
		t.Fatalf("again: %q %v", again, err)
	}
}

func TestSquareRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, other, holder := e.account(t), e.account(t), uuid.NewV7()
	photo := e.upload(t, owner, jpegBytes(t, 80, 60))
	if err := e.media.Attach(ctx, owner, holder, []uuid.UUID{photo}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		owner, id uuid.UUID
	}{
		"another owner's": {other, e.upload(t, owner, jpegBytes(t, 80, 60))},
		"unknown":         {owner, uuid.NewV7()},
		"not a photo":     {owner, e.upload(t, owner, []byte("hello, not a photo"))},
		"held as a photo": {owner, photo},
	} {
		if _, err := e.media.Square(ctx, c.owner, holder, c.id); !errors.Is(err, domain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

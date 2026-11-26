package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/platform/objectstore"
)

// A frame the server saves is a waiting photo upload of the seller's: it is
// read back as a search-sized JPEG, a listing attaches and processes it,
// and a retried save neither fails nor brings its original back.
//
// UC-SHR-01 A3.
func TestSavePhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, other := accounttest.New(t, e.db), accounttest.New(t, e.db)
	frame, id := mediatest.JPEG(t, 1280, 720), uuid.NewV7()
	if err := e.media.SavePhoto(ctx, owner, id, frame, "image/jpeg", domain.OriginFrame); err != nil {
		t.Fatal(err)
	}
	if err := e.media.SavePhoto(ctx, owner, id, frame, "image/jpeg", domain.OriginFrame); err != nil {
		t.Fatalf("a retried save: %v", err)
	}
	u, err := repository.NewUploads(e.db).Get(ctx, id)
	if err != nil || u.Status() != domain.StatusWaiting || u.OwnerID() != owner || u.ContentType() != "image/jpeg" ||
		u.Size() != int64(len(frame)) || u.Origin() != domain.OriginFrame {
		t.Fatalf("%+v %v", u.State(), err)
	}
	if img, imgErr := e.media.QueryImage(ctx, owner, id); imgErr != nil || len(img) == 0 {
		t.Fatalf("read back for the model: %v", imgErr)
	}
	if err = e.media.SavePhoto(ctx, other, id, frame, "image/jpeg", domain.OriginFrame); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("the same id for another owner: %v", err)
	}
	if err = e.media.SavePhoto(ctx, owner, uuid.NewV7(), nil, "image/jpeg", domain.OriginFrame); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("an empty frame: %v", err)
	}
	listing := uuid.NewV7()
	if err = e.media.Attach(ctx, owner, listing, []uuid.UUID{id}, uuid.NewV7()); err != nil {
		t.Fatalf("a listing takes the frame: %v", err)
	}
	if res := e.process(t, id); res.Status != domain.StatusReady || res.PhotoKey == "" {
		t.Fatalf("processed: %+v", res)
	}
	if err = e.media.SavePhoto(ctx, owner, id, frame, "image/jpeg", domain.OriginFrame); err != nil {
		t.Fatalf("a save after the listing took the frame: %v", err)
	}
	if _, _, err = e.store.Stat(ctx, domain.UploadKey(id)); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("the processed frame's original came back: %v", err)
	}
}

// A saved frame waits 24 hours like any upload, then expires.
func TestSavedPhotoExpires(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, id := accounttest.New(t, e.db), uuid.NewV7()
	if err := e.media.SavePhoto(ctx, owner, id, mediatest.JPEG(t, 64, 48), "image/jpeg", domain.OriginFrame); err != nil {
		t.Fatal(err)
	}
	e.expire(t, id)
	if u, _ := repository.NewUploads(e.db).Get(ctx, id); u.Status() != domain.StatusExpired {
		t.Fatalf("status %s", u.Status())
	}
}

// A presigned frame URL reads the frame without credentials.
func TestPresignPhoto(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, id, frame := accounttest.New(t, e.db), uuid.NewV7(), mediatest.JPEG(t, 64, 48)
	if err := e.media.SavePhoto(ctx, owner, id, frame, "image/jpeg", domain.OriginFrame); err != nil {
		t.Fatal(err)
	}
	url, err := e.media.PresignPhoto(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !bytes.Equal(got, frame) {
		t.Fatalf("%d, %d bytes", res.StatusCode, len(got))
	}
}

// A read holds one video of its reader's, opens it, and consumes it at the
// end; nothing else holds it, MediaExpire leaves it, and once consumed it is
// gone for good.
//
// UC-SHR-01 A5.
func TestHoldOpenConsumeVideo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, other, read := accounttest.New(t, e.db), accounttest.New(t, e.db), uuid.NewV7()
	body := []byte("an mp4's bytes")
	video := mediatest.Upload(t, e.media, owner, domain.ContentTypeMP4, body)
	photo := e.upload(t, owner, mediatest.JPEG(t, 64, 48))
	notPut, err := e.media.CreateUpload(ctx, owner, domain.ContentTypeMP4, 10)
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"another owner's": func() error { return e.media.HoldVideo(ctx, other, read, video, uuid.NewV7()) },
		"a photo":         func() error { return e.media.HoldVideo(ctx, owner, read, photo, uuid.NewV7()) },
		"never put":       func() error { return e.media.HoldVideo(ctx, owner, read, notPut.UploadID, uuid.NewV7()) },
		"unknown":         func() error { return e.media.HoldVideo(ctx, owner, read, uuid.NewV7(), uuid.NewV7()) },
	} {
		if callErr := call(); !errors.Is(callErr, domain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, callErr)
		}
	}
	held := uuid.NewV7()
	if err = e.media.HoldVideo(ctx, owner, read, video, held); err != nil {
		t.Fatal(err)
	}
	if err = e.media.HoldVideo(ctx, owner, read, video, uuid.NewV7()); err != nil {
		t.Fatalf("held again by the same read: %v", err)
	}
	// The hold saves under the caller's write ID; the repeat writes nothing.
	if u, _ := repository.NewUploads(e.db).Get(ctx, video); u.WriteID() != held {
		t.Fatalf("write ID %s, want %s", u.WriteID(), held)
	}
	if err = e.media.HoldVideo(ctx, owner, uuid.NewV7(), video, uuid.NewV7()); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a second read of the video: %v", err)
	}
	e.expire(t, video)
	key, err := e.media.VideoKey(ctx, video)
	if err != nil {
		t.Fatalf("key after an expiry run: %v", err)
	}
	r, err := e.store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if !bytes.Equal(got, body) {
		t.Fatalf("%q", got)
	}
	for name, id := range map[string]uuid.UUID{"a photo": photo, "unknown": uuid.NewV7()} {
		if _, keyErr := e.media.VideoKey(ctx, id); !errors.Is(keyErr, objectstore.ErrNotFound) {
			t.Errorf("key of %s: %v", name, keyErr)
		}
	}
	consumed := uuid.NewV7()
	if err = e.media.Consume(ctx, video, consumed); err != nil {
		t.Fatal(err)
	}
	if err = e.media.Consume(ctx, video, uuid.NewV7()); err != nil {
		t.Fatalf("consumed twice: %v", err)
	}
	if err = e.media.Consume(ctx, uuid.NewV7(), uuid.NewV7()); err != nil {
		t.Fatalf("consume unknown: %v", err)
	}
	if u, _ := repository.NewUploads(e.db).Get(ctx, video); u.Status() != domain.StatusConsumed || u.WriteID() != consumed {
		t.Fatalf("status %s, write ID %s", u.Status(), u.WriteID())
	}
	if _, _, err = e.store.Stat(ctx, domain.UploadKey(video)); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("object after consume: %v", err)
	}
	if _, err = e.media.VideoKey(ctx, video); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("key after consume: %v", err)
	}
	if err = e.media.HoldVideo(ctx, owner, read, video, uuid.NewV7()); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("held after consume: %v", err)
	}
}

// A copied photo keeps its type and origin; the server never saves an
// app's upload or a video this way.
func TestSavePhotoOrigins(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := accounttest.New(t, e.db)
	id := uuid.NewV7()
	if err := e.media.SavePhoto(ctx, owner, id, webpBytes(t), "image/webp", domain.OriginLink); err != nil {
		t.Fatal(err)
	}
	u, err := repository.NewUploads(e.db).Get(ctx, id)
	if err != nil || u.ContentType() != "image/webp" || u.Origin() != domain.OriginLink || u.Status() != domain.StatusWaiting {
		t.Fatalf("%+v %v", u.State(), err)
	}
	if err = e.media.SavePhoto(ctx, owner, uuid.NewV7(), webpBytes(t), "image/webp", domain.OriginApp); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("origin app: %v", err)
	}
	if err = e.media.SavePhoto(ctx, owner, uuid.NewV7(), []byte("mp4"), domain.ContentTypeMP4, domain.OriginLink); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("a video: %v", err)
	}
}

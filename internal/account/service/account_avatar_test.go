package service_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// withMedia is the account service on a database with module media and
// its flows on the test stack.
func withMedia(t *testing.T) (*service.Account, *mediasvc.Media) {
	t.Helper()
	db := pgtest.New(t)
	url, _, err := restatetest.URLs()
	if err != nil {
		t.Fatal(err)
	}
	media := mediasvc.New(mediasvc.Deps{
		Uploads: mediarepo.NewUploads(db), Store: objectstoretest.New(t), Ingress: ingress.NewClient(url),
		Prefix: restatetest.Prefix(), Now: time.Now,
	})
	restatetest.Start(t, media.Durable()...)
	return service.New(repository.NewAccounts(db), media, time.Now), media
}

// put uploads body as a JPEG of owner's.
func put(t *testing.T, media *mediasvc.Media, owner uuid.UUID, body []byte) uuid.UUID {
	t.Helper()
	ticket, err := media.CreateUpload(context.Background(), owner, "image/jpeg", int64(len(body)))
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
	return ticket.UploadID
}

func photoBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 600, 450)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The reply to an avatar change already carries the processed square; a
// later update removes it.
func TestUpdateAvatar(t *testing.T) {
	t.Parallel()
	accounts, media := withMedia(t)
	ctx := context.Background()
	id := uuid.NewV7()
	if err := accounts.Create(ctx, id, "Linh"); err != nil {
		t.Fatal(err)
	}
	upload := put(t, media, id, photoBytes(t))
	v, err := accounts.Update(ctx, id, service.UpdateInput{Avatar: &upload})
	if err != nil || !strings.HasSuffix(v.AvatarURL, "media/"+upload.String()+".jpg") {
		t.Fatalf("%+v %v", v, err)
	}
	if got, _ := accounts.Get(ctx, id); got.AvatarURL != v.AvatarURL {
		t.Fatalf("read back %q", got.AvatarURL)
	}
	if v, err = accounts.Update(ctx, id, service.UpdateInput{RemoveAvatar: true}); err != nil || v.AvatarURL != "" {
		t.Fatalf("removed: %+v %v", v, err)
	}
}

// A photo media refuses, or someone else's upload, is invalid_upload and
// leaves the account as it was.
func TestUpdateAvatarRefusals(t *testing.T) {
	t.Parallel()
	accounts, media := withMedia(t)
	ctx := context.Background()
	id, other := uuid.NewV7(), uuid.NewV7()
	for _, a := range []uuid.UUID{id, other} {
		if err := accounts.Create(ctx, a, "Linh"); err != nil {
			t.Fatal(err)
		}
	}
	for name, upload := range map[string]uuid.UUID{
		"not a photo":     put(t, media, id, []byte("hello, not a photo")),
		"another owner's": put(t, media, other, photoBytes(t)),
	} {
		_, err := accounts.Update(ctx, id, service.UpdateInput{DisplayName: new("Minh"), Avatar: &upload})
		if !errors.Is(err, mediadomain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if v, _ := accounts.Get(ctx, id); v.DisplayName != "Linh" || v.AvatarURL != "" {
		t.Fatalf("changed: %+v", v)
	}
}

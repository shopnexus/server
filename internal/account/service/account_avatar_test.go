package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/account/service"
	mediadomain "github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/mediatest"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// withMedia is the account service on a database with module media and
// its flows on the test stack, with the store it reads.
func withMedia(t *testing.T) (*service.Account, repository.Accounts, *mediasvc.Media) {
	t.Helper()
	db := pgtest.New(t)
	media := mediatest.Service(db, objectstoretest.New(t), restatetest.Ingress(t), restatetest.Prefix())
	restatetest.Start(t, media.Durable()...)
	store := repository.NewAccounts(db)
	return service.New(service.Deps{Accounts: store, Media: media, Now: time.Now}), store, media
}

// The reply to an avatar change already carries the processed square; a
// later update removes it.
func TestUpdateAvatar(t *testing.T) {
	t.Parallel()
	accounts, store, media := withMedia(t)
	ctx := context.Background()
	id := seed(t, store)
	upload := mediatest.Upload(t, media, id, "image/jpeg", mediatest.JPEG(t, 600, 450))
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
	accounts, store, media := withMedia(t)
	ctx := context.Background()
	id, other := seed(t, store), seed(t, store)
	for name, upload := range map[string]uuid.UUID{
		"not a photo":     mediatest.Upload(t, media, id, "image/jpeg", []byte("hello, not a photo")),
		"another owner's": mediatest.Upload(t, media, other, "image/jpeg", mediatest.JPEG(t, 600, 450)),
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

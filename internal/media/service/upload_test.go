package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/platform/objectstore"
)

func TestCreateUploadIsWaitingWithAPresignedPut(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner := e.account(t)
	ticket, err := e.media.CreateUpload(context.Background(), owner, "image/png", 12)
	if err != nil || ticket.Headers["Content-Type"] != "image/png" || ticket.ExpiresAt.Before(time.Now()) {
		t.Fatalf("%+v %v", ticket, err)
	}
	u, err := repository.NewUploads(e.db).Get(context.Background(), ticket.UploadID)
	if err != nil || u.Status() != domain.StatusWaiting || u.OwnerID() != owner {
		t.Fatalf("%+v %v", u.State(), err)
	}
	if _, err := e.media.CreateUpload(context.Background(), owner, "image/heic", 12); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("heic: %v", err)
	}
}

func TestAttachRefusals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner, other, holder := e.account(t), e.account(t), uuid.NewV7()
	mine := e.upload(t, owner, []byte("jpeg bytes"))
	theirs := e.upload(t, other, []byte("jpeg bytes"))
	notPut, err := e.media.CreateUpload(ctx, owner, "image/jpeg", 10)
	if err != nil {
		t.Fatal(err)
	}
	for name, ids := range map[string][]uuid.UUID{
		"another owner's": {mine, theirs},
		"never put":       {mine, notPut.UploadID},
		"unknown":         {mine, uuid.NewV7()},
	} {
		if err := e.media.Attach(ctx, owner, holder, ids); !errors.Is(err, domain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if u, _ := repository.NewUploads(e.db).Get(ctx, mine); u.Status() != domain.StatusWaiting {
		t.Fatalf("a refused attach attached %s", u.Status())
	}
	if err := e.media.Attach(ctx, owner, holder, []uuid.UUID{mine}); err != nil {
		t.Fatal(err)
	}
	if err := e.media.Attach(ctx, owner, holder, []uuid.UUID{mine}); err != nil {
		t.Fatalf("a retried attach: %v", err)
	}
	if err := e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{mine}); !errors.Is(err, domain.ErrInvalidUpload) {
		t.Fatalf("attached to another holder: %v", err)
	}
}

func TestExpireDeletesAWaitingUpload(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	id := e.upload(t, e.account(t), []byte("jpeg bytes"))
	e.expire(t, id)
	u, _ := repository.NewUploads(e.db).Get(ctx, id)
	if u.Status() != domain.StatusExpired {
		t.Fatalf("status %s", u.Status())
	}
	if _, _, err := e.store.Stat(ctx, u.Key()); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("object: %v", err)
	}
}

func TestExpireLeavesAttachedUploads(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	owner := e.account(t)
	id := e.upload(t, owner, []byte("jpeg bytes"))
	if err := e.media.Attach(ctx, owner, uuid.NewV7(), []uuid.UUID{id}); err != nil {
		t.Fatal(err)
	}
	e.expire(t, id)
	u, _ := repository.NewUploads(e.db).Get(ctx, id)
	if u.Status() != domain.StatusAttached {
		t.Fatalf("status %s", u.Status())
	}
	if _, _, err := e.store.Stat(ctx, u.Key()); err != nil {
		t.Fatalf("object: %v", err)
	}
	e.expire(t, uuid.NewV7()) // an unknown upload is nothing to do
}

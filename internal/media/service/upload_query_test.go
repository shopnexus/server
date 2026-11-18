package service_test

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

func TestQueryUpload(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	store := objectstoretest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	m := mediatest.Service(db, store, in, p)
	restatetest.Start(t, m.Durable()...)
	ctx := context.Background()
	me, other := accounttest.New(t, db), accounttest.New(t, db)

	mine := mediatest.Upload(t, m, me, "image/jpeg", mediatest.JPEG(t, 2000, 1000))
	q, err := m.QueryUpload(ctx, me, mine)
	if err != nil || q.ExpiresAt.Before(time.Now().Add(23*time.Hour)) {
		t.Fatalf("%+v %v", q, err)
	}
	img, err := m.QueryImage(ctx, me, mine)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(img))
	if err != nil || cfg.Width != 768 || cfg.Height != 384 {
		t.Fatalf("%+v %v", cfg, err)
	}

	never, err := m.CreateUpload(ctx, me, "image/jpeg", 1000) // no PUT
	if err != nil {
		t.Fatal(err)
	}
	attached := mediatest.Upload(t, m, me, "image/jpeg", mediatest.JPEG(t, 64, 48))
	if err = m.Attach(ctx, me, uuid.NewV7(), []uuid.UUID{attached}); err != nil {
		t.Fatal(err)
	}
	later := service.New(service.Deps{
		Uploads: repository.NewUploads(db), Store: store, Ingress: in, Prefix: p,
		Now: func() time.Time { return time.Now().Add(domain.ExpireAfter + time.Minute) },
	})
	for name, call := range map[string]func() error{
		"someone else's": func() error { _, e := m.QueryUpload(ctx, other, mine); return e },
		"never put":      func() error { _, e := m.QueryUpload(ctx, me, never.UploadID); return e },
		"attached":       func() error { _, e := m.QueryUpload(ctx, me, attached); return e },
		"expired":        func() error { _, e := later.QueryUpload(ctx, me, mine); return e },
		"unknown":        func() error { _, e := m.QueryUpload(ctx, me, uuid.NewV7()); return e },
		"image of other": func() error { _, e := m.QueryImage(ctx, other, mine); return e },
	} {
		if err := call(); !errors.Is(err, domain.ErrInvalidUpload) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestReadPhoto(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	store := objectstoretest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	m := mediatest.Service(db, store, in, p)
	restatetest.Start(t, m.Durable()...)
	ctx := context.Background()
	me := accounttest.New(t, db)
	id := mediatest.Upload(t, m, me, "image/jpeg", mediatest.JPEG(t, 64, 48))
	key, err := m.Square(ctx, me, uuid.NewV7(), id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.ReadPhoto(ctx, key)
	if err != nil || !bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) {
		t.Fatalf("%d bytes %v", len(b), err)
	}
	if _, err = m.ReadPhoto(ctx, "media/none.jpg"); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatal(err)
	}
}

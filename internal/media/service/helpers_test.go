package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/media/repository"
	"github.com/shopnexus/server/internal/media/service"
	"github.com/shopnexus/server/internal/platform/objectstore"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
)

type env struct {
	db      *pgxpool.Pool
	store   *objectstore.Store
	media   *service.Media
	ingress *ingress.Client
	prefix  string
	// clock is media's clock; only expire and the tests of extensions move it.
	clock *restatetest.Clock
}

// newEnv serves module media's Restate services on the test stack, on a
// database and the test bucket, with a stopped clock.
func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	store := objectstoretest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	clock := restatetest.NewClock(time.Now())
	m := service.New(service.Deps{Uploads: repository.NewUploads(db), Store: store, Ingress: in, Prefix: p, Now: clock.Now})
	restatetest.Start(t, m.Durable()...)
	return env{db: db, store: store, media: m, ingress: in, prefix: p, clock: clock}
}

// upload creates a JPEG upload for owner and PUTs body to it.
func (e env) upload(t *testing.T, owner uuid.UUID, body []byte) uuid.UUID {
	t.Helper()
	return mediatest.Upload(t, e.media, owner, "image/jpeg", body)
}

// runExpire runs MediaExpire for id at the clock's time.
func (e env) runExpire(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := ingress.Service[uuid.UUID, restate.Void](e.ingress, e.prefix+"MediaExpire", "Run").
		Request(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

// expire moves the clock a day on and runs MediaExpire for id, as Restate
// does a day after the upload was created.
func (e env) expire(t *testing.T, id uuid.UUID) {
	t.Helper()
	e.clock.Add(domain.ExpireAfter)
	e.runExpire(t, id)
}

// get reads an upload.
func (e env) get(t *testing.T, id uuid.UUID) *domain.Upload {
	t.Helper()
	u, err := repository.NewUploads(e.db).Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return &u
}

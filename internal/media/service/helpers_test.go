package service_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type env struct {
	db      *pgxpool.Pool
	store   *objectstore.Store
	media   *service.Media
	ingress *ingress.Client
	prefix  string
}

// newEnv serves module media's Restate services on the test stack, on a
// database and the test bucket.
func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	store := objectstoretest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	m := mediatest.Service(db, store, in, p)
	restatetest.Start(t, m.Durable()...)
	return env{db: db, store: store, media: m, ingress: in, prefix: p}
}

// upload creates a JPEG upload for owner and PUTs body to it.
func (e env) upload(t *testing.T, owner uuid.UUID, body []byte) uuid.UUID {
	t.Helper()
	return mediatest.Upload(t, e.media, owner, "image/jpeg", body)
}

// expire runs MediaExpire for id now, as Restate does 24 hours after the
// upload was created.
func (e env) expire(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := ingress.Service[uuid.UUID, restate.Void](e.ingress, e.prefix+"MediaExpire", "Run").
		Request(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

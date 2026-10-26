package service_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/media/repository"
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
	url, _, err := restatetest.URLs()
	if err != nil {
		t.Fatal(err)
	}
	in, p := ingress.NewClient(url), restatetest.Prefix()
	m := service.New(service.Deps{Uploads: repository.NewUploads(db), Store: store, Ingress: in, Prefix: p, Now: time.Now})
	restatetest.Start(t, m.Durable()...)
	return env{db: db, store: store, media: m, ingress: in, prefix: p}
}

func (e env) account(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := e.db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

// upload creates a JPEG upload for owner and PUTs body to it.
func (e env) upload(t *testing.T, owner uuid.UUID, body []byte) uuid.UUID {
	t.Helper()
	ticket, err := e.media.CreateUpload(context.Background(), owner, "image/jpeg", int64(len(body)))
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
	if res.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", res.StatusCode)
	}
	return ticket.UploadID
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

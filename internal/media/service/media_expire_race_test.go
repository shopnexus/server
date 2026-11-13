package service_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/media/domain"
	"github.com/shopnexus/server2/internal/media/mediatest"
	"github.com/shopnexus/server2/internal/media/repository"
	"github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

// racingUploads runs race once, just before the first Save, as a write that
// lands between MediaExpire's load and its save.
type racingUploads struct {
	repository.Uploads
	once sync.Once
	race func()
}

func (r *racingUploads) Save(ctx context.Context, u *domain.Upload, writeID uuid.UUID) error {
	r.once.Do(r.race)
	return r.Uploads.Save(ctx, u, writeID)
}

// MediaExpire that loses its save to an extension reads the upload again
// and goes on from what it finds: here the extension's end has come, so it
// expires the upload. Giving up would leave the upload and its object for
// good, since nothing else wakes for it.
func TestExpireThatLosesToAnExtensionGoesOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	store := objectstoretest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	clock := restatetest.NewClock(time.Now())
	var id uuid.UUID
	uploads := &racingUploads{Uploads: repository.NewUploads(db), race: func() {
		if _, err := db.Exec(ctx, `UPDATE uploads SET expires_at = expires_at + interval '1 second', write_id = gen_random_uuid() WHERE id = @id`,
			pgx.StrictNamedArgs{"id": id}); err != nil {
			t.Error(err)
		}
		clock.Add(time.Second)
	}}
	m := service.New(service.Deps{Uploads: uploads, Store: store, Ingress: in, Prefix: p, Now: clock.Now})
	restatetest.Start(t, m.Durable()...)
	id = mediatest.Upload(t, m, accounttest.New(t, db), "image/jpeg", []byte("jpeg bytes"))
	clock.Add(domain.ExpireAfter)
	if _, err := ingress.Service[uuid.UUID, restate.Void](in, p+"MediaExpire", "Run").Request(ctx, id); err != nil {
		t.Fatal(err)
	}
	u, err := repository.NewUploads(db).Get(ctx, id)
	if err != nil || u.Status() != domain.StatusExpired {
		t.Fatalf("after a lost save: %s %v", u.Status(), err)
	}
}

// A run that wakes a moment before the expiry, by its clock against the
// sender's, waits out the rest itself: a send keyed for the same moment
// would attach to this very run and nothing would wake again.
func TestExpireThatWakesAMomentEarlyWaitsItOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	store := objectstoretest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	var offset time.Duration
	var mu sync.Mutex
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return time.Now().Add(offset)
	}
	m := service.New(service.Deps{Uploads: repository.NewUploads(db), Store: store, Ingress: in, Prefix: p, Now: now})
	restatetest.Start(t, m.Durable()...)
	id := mediatest.Upload(t, m, accounttest.New(t, db), "image/jpeg", []byte("jpeg bytes"))
	u, err := repository.NewUploads(db).Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	offset = time.Until(u.ExpiresAt()) - 500*time.Millisecond // half a second before the expiry
	mu.Unlock()
	if _, err = ingress.Service[uuid.UUID, restate.Void](in, p+"MediaExpire", "Run").Request(ctx, id); err != nil {
		t.Fatal(err)
	}
	if u, err = repository.NewUploads(db).Get(ctx, id); err != nil || u.Status() != domain.StatusExpired {
		t.Fatalf("a run half a second early: %s %v", u.Status(), err)
	}
}

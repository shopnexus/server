package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/restatedev/sdk-go/ingress"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/media/mediatest"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server2/internal/platform/testenv"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	media    *mediasvc.Media
	catalog  *service.Catalog
	listings repository.Listings
	ingress  *ingress.Client
	prefix   string
}

// newEnv serves modules media and catalog on the test stack, on a database
// and the test bucket. Each option changes the catalog's dependencies, to
// wrap a store or a service with a fault.
func newEnv(t *testing.T, opts ...func(*service.Deps)) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	m := mediatest.Service(db, objectstoretest.New(t), in, p)
	c := catalogtest.Service(db, accounts, m, in, p, opts...)
	restatetest.Start(t, append(m.Durable(), c.Durable()...)...)
	return env{db: db, accounts: accounts, media: m, catalog: c, listings: repository.NewListings(db), ingress: in, prefix: p}
}

// seller creates an account with a verified phone number.
func (e env) seller(t *testing.T) uuid.UUID {
	t.Helper()
	return accounttest.WithPhone(t, e.db)
}

// account creates an account with no sign-in method.
func (e env) account(t *testing.T) uuid.UUID {
	t.Helper()
	return accounttest.New(t, e.db)
}

// upload creates a JPEG upload for owner and PUTs body to it.
func (e env) upload(t *testing.T, owner uuid.UUID, body []byte) uuid.UUID {
	t.Helper()
	return mediatest.Upload(t, e.media, owner, "image/jpeg", body)
}

// draft is a valid listing with the given photos.
func draft(photos ...uuid.UUID) domain.Draft {
	return domain.Draft{
		Title: "iPhone 13 128GB", Price: 9_500_000, Description: "Máy zin", CategoryCode: "phones.iphone",
		Condition: domain.ConditionLikeNew, AllowOffers: true, CommuneCode: "00004", Photos: photos,
	}
}

// post posts a listing with one fresh photo for seller.
func (e env) post(t *testing.T, seller uuid.UUID) service.ListingView {
	t.Helper()
	v, err := e.catalog.Post(context.Background(), seller, uuid.NewV7().String(), draft(e.upload(t, seller, mediatest.JPEG(t, 64, 48))))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// settle waits until listing id leaves processing and returns it as its
// seller sees it.
func (e env) settle(t *testing.T, seller, id uuid.UUID) service.ListingView {
	t.Helper()
	var v service.ListingView
	testenv.Eventually(t, 30*time.Second, func() error {
		var err error
		if v, err = e.catalog.Get(context.Background(), &seller, id); err != nil {
			t.Fatal(err)
		}
		if v.Status == domain.StatusProcessing {
			return fmt.Errorf("listing %s still processing", id)
		}
		return nil
	})
	return v
}

// active posts a listing and waits until it is on sale.
func (e env) active(t *testing.T, seller uuid.UUID) service.ListingView {
	t.Helper()
	v := e.settle(t, seller, e.post(t, seller).ID)
	if v.Status != domain.StatusActive {
		t.Fatalf("%+v", v)
	}
	return v
}

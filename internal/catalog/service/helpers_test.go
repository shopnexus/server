package service_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/catalog/service"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	media    *mediasvc.Media
	catalog  *service.Catalog
	listings repository.Listings
}

// newEnv serves modules media and catalog on the test stack, on a database
// and the test bucket. Each option changes the catalog's dependencies, to
// wrap a store or a service with a fault.
func newEnv(t *testing.T, opts ...func(*service.Deps)) env {
	t.Helper()
	db := pgtest.New(t)
	store := objectstoretest.New(t)
	url, _, err := restatetest.URLs()
	if err != nil {
		t.Fatal(err)
	}
	in, p := ingress.NewClient(url), restatetest.Prefix()
	accounts := accountsvc.New(accountsvc.Deps{Accounts: accountrepo.NewAccounts(db), Now: time.Now})
	m := mediasvc.New(mediasvc.Deps{Uploads: mediarepo.NewUploads(db), Store: store, Ingress: in, Prefix: p, Now: time.Now})
	listings := repository.NewListings(db)
	deps := service.Deps{
		Listings: listings, Categories: repository.NewCategories(db), Regions: repository.NewRegions(db),
		Searches: repository.NewSearches(db),
		Accounts: accounts, Media: m,
		Ingress: in, Prefix: p, Now: time.Now,
	}
	for _, o := range opts {
		o(&deps)
	}
	c := service.New(deps)
	restatetest.Start(t, append(m.Durable(), c.Durable()...)...)
	return env{db: db, accounts: accounts, media: m, catalog: c, listings: listings}
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

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := range 48 {
		for x := range 64 {
			m.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
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
	v, err := e.catalog.Post(context.Background(), seller, uuid.NewV7().String(), draft(e.upload(t, seller, jpegBytes(t))))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// settle waits until listing id leaves processing and returns it as its
// seller sees it.
func (e env) settle(t *testing.T, seller, id uuid.UUID) service.ListingView {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		v, err := e.catalog.Get(context.Background(), &seller, id)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status != domain.StatusProcessing {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("listing %s still processing", id)
		}
		time.Sleep(100 * time.Millisecond)
	}
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

// kindsOf lists the kinds of the events about subject, oldest first.
func kindsOf(t *testing.T, db *pgxpool.Pool, subject uuid.UUID) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT kind FROM events WHERE subject_id = @id ORDER BY id`, pgx.StrictNamedArgs{"id": subject})
	if err != nil {
		t.Fatal(err)
	}
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return kinds
}

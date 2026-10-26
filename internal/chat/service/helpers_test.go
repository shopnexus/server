package service_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restatedev/sdk-go/ingress"

	accountrepo "github.com/shopnexus/server2/internal/account/repository"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	authdomain "github.com/shopnexus/server2/internal/auth/domain"
	authrepo "github.com/shopnexus/server2/internal/auth/repository"
	authsvc "github.com/shopnexus/server2/internal/auth/service"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	catalogrepo "github.com/shopnexus/server2/internal/catalog/repository"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/chat/service"
	dealrepo "github.com/shopnexus/server2/internal/deal/repository"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	mediarepo "github.com/shopnexus/server2/internal/media/repository"
	mediasvc "github.com/shopnexus/server2/internal/media/service"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/contact"
	"github.com/shopnexus/server2/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/platform/restatex/restatetest"
)

type env struct {
	db       *pgxpool.Pool
	accounts accountsvc.Service
	catalog  *catalogsvc.Catalog
	listings catalogrepo.Listings
	media    *mediasvc.Media
	deals    *dealsvc.Deals
	deps     service.Deps
	chat     *service.Chat
}

// newEnv builds chat and the modules it calls on a database and the test
// bucket, and serves media's and chat's flows under one prefix.
func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	url, _, err := restatetest.URLs()
	if err != nil {
		t.Fatal(err)
	}
	in, p := ingress.NewClient(url), restatetest.Prefix()
	accounts := accountsvc.New(accountrepo.NewAccounts(db), time.Now)
	auth := authsvc.New(authsvc.Deps{Credentials: authrepo.NewCredentials(db)})
	media := mediasvc.New(mediasvc.Deps{
		Uploads: mediarepo.NewUploads(db), Store: objectstoretest.New(t), Ingress: in, Prefix: p, Now: time.Now,
	})
	listings := catalogrepo.NewListings(db)
	c := catalogsvc.New(catalogsvc.Deps{
		Listings: listings, Categories: catalogrepo.NewCategories(db), Regions: catalogrepo.NewRegions(db),
		Accounts: accounts, Auth: auth, Media: media, Ingress: in, Prefix: p, Now: time.Now,
	})
	deals := dealsvc.New(dealsvc.Deps{
		Deals: dealrepo.NewDeals(db), Accounts: accounts, Auth: auth, Catalog: c, Ingress: in, Prefix: p, Now: time.Now,
	})
	deps := service.Deps{
		Chats: repository.NewChats(db), Accounts: accounts, Auth: auth, Catalog: c, Media: media, Deals: deals,
		Ingress: in, Prefix: p, Now: time.Now,
	}
	chat := service.New(deps)
	restatetest.Start(t, append(media.Durable(), chat.Durable()...)...)
	return env{db: db, accounts: accounts, catalog: c, listings: listings, media: media, deals: deals, deps: deps, chat: chat}
}

// person creates an account with a verified phone number.
func (e env) person(t *testing.T) uuid.UUID {
	t.Helper()
	id := e.emailOnly(t)
	phone, err := contact.ParsePhone(fmt.Sprintf("09%08d", rand.IntN(100_000_000)))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	creds := authdomain.NewCredentials(id, authdomain.NewIdentity(uuid.NewV7(), phone, now), now)
	if err = authrepo.NewCredentials(e.db).Create(context.Background(), creds); err != nil {
		t.Fatal(err)
	}
	return id
}

// emailOnly creates an account with no phone number.
func (e env) emailOnly(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if err := e.accounts.Create(context.Background(), id, "Linh"); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e env) suspend(t *testing.T, id uuid.UUID) {
	t.Helper()
	if _, err := e.accounts.Suspend(context.Background(), accountsvc.SuspendInput{ID: id, Reason: "spam", By: audit.ByCLI("test")}); err != nil {
		t.Fatal(err)
	}
}

// listing puts a listing of seller's on sale and returns its ID.
func (e env) listing(t *testing.T, seller uuid.UUID) uuid.UUID {
	t.Helper()
	photo := uuid.NewV7()
	now := time.Now()
	l, err := catalogdomain.NewListing(uuid.NewV7(), seller, catalogdomain.Draft{
		Title: "Xe đạp Asama", Price: 1_500_000, CategoryCode: "vehicles.bicycle", Condition: catalogdomain.ConditionUsed,
		AllowOffers: true, CommuneCode: "00004", Photos: []uuid.UUID{photo},
	}, catalogdomain.Refs{CategoryIsLeaf: true, CommuneExists: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	l.Published(map[uuid.UUID]catalogdomain.PhotoKeys{photo: {Photo: "media/a.jpg", Thumb: "media/a_thumb.jpg"}}, now)
	if err = e.listings.Insert(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	return l.ID()
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

func key() string { return uuid.NewV7().String() }

// open has buyer open a conversation from listing with text.
func (e env) open(t *testing.T, buyer, listing uuid.UUID, text string) service.ConversationView {
	t.Helper()
	v, err := e.chat.Open(context.Background(), buyer, key(), service.OpenInput{ListingID: listing, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// messages lists what viewer sees of a conversation, newest first.
func (e env) messages(t *testing.T, viewer, conversation uuid.UUID) []service.MessageView {
	t.Helper()
	p, err := e.chat.Messages(context.Background(), viewer, conversation, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	return p.Items
}

// settled waits until message id leaves processing for its sender.
func (e env) settled(t *testing.T, sender, conversation, id uuid.UUID) service.MessageView {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		ms := e.messages(t, sender, conversation)
		for i := range ms {
			if ms[i].ID == id && ms[i].Status != domain.StatusProcessing {
				return ms[i]
			}
		}
	}
	t.Fatalf("message %s still processing", id)
	return service.MessageView{}
}

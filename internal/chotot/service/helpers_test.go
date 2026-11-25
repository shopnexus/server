package service_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/account/accounttest"
	assistdomain "github.com/shopnexus/server/internal/assist/domain"
	assistsvc "github.com/shopnexus/server/internal/assist/service"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chotot/repository"
	"github.com/shopnexus/server/internal/chotot/service"
	"github.com/shopnexus/server/internal/media/mediatest"
	"github.com/shopnexus/server/internal/platform/objectstore/objectstoretest"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/restatex/restatetest"
	"github.com/shopnexus/server/internal/provider/chotot"
)

type env struct {
	db      *pgxpool.Pool
	svc     *service.Chotot
	catalog *catalogsvc.Catalog
	source  *fakeSource
	checks  *fakeChecks
	ingress *ingress.Client
	prefix  string
}

// newEnv serves modules media, catalog and chotot on the test stack, with
// a fake Chợ Tốt and a fill that copies the listing as it is.
func newEnv(t *testing.T) env {
	t.Helper()
	db := pgtest.New(t)
	in, p := restatetest.Ingress(t), restatetest.Prefix()
	accounts := accounttest.Service(db)
	m := mediatest.Service(db, objectstoretest.New(t), in, p)
	c := catalogtest.Service(db, accounts, m, in, p)
	e := env{db: db, catalog: c, source: newSource(t), checks: &fakeChecks{}, ingress: in, prefix: p}
	e.svc = service.New(service.Deps{
		Codes: repository.NewCodes(db), Links: repository.NewLinks(db), Imports: repository.NewImports(db),
		Source: e.source, Fill: fakeFill{}, Accounts: accounts, Catalog: c, Media: m, Checks: e.checks,
		CallTimeout: 5 * time.Second, SyncEvery: time.Hour, Log: slog.New(slog.DiscardHandler),
		Ingress: in, Prefix: p, Now: time.Now,
	})
	restatetest.Start(t, slices.Concat(m.Durable(), c.Durable(), e.svc.Durable())...)
	return e
}

// seller is an account with a phone and one listing on sale, in commune
// 00004.
func (e env) seller(t *testing.T) uuid.UUID {
	t.Helper()
	me := accounttest.WithPhone(t, e.db)
	catalogtest.Listing(t, e.db, me)
	return me
}

// oid is a profile ID of its own for each call.
func oid() string { return strings.ReplaceAll(uuid.NewV7().String(), "-", "") }

// profileURL is the address of profile.
func profileURL(profile string) string { return "https://www.chotot.com/user/" + profile }

// link gives profile a bio with a fresh code of account's and links it.
func (e env) link(t *testing.T, account uuid.UUID, profile string) service.LinkView {
	t.Helper()
	code, err := e.svc.Code(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}
	e.source.setBio(profile, "Shop đồ cũ. "+code.Code)
	v, err := e.svc.Link(context.Background(), account, profileURL(profile))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// sync runs one check of account's link and waits for it.
func (e env) sync(t *testing.T, account uuid.UUID) {
	t.Helper()
	l, found, err := repository.NewLinks(e.db).Get(context.Background(), account)
	if err != nil || !found {
		t.Fatalf("no link: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err = ingress.Service[service.SyncInput, restate.Void](e.ingress, e.prefix+service.SyncService, "Run").
		Request(ctx, service.SyncInput{Link: l.ID}); err != nil {
		t.Fatal(err)
	}
}

// count runs a count query.
func (e env) count(t *testing.T, query string, args pgx.StrictNamedArgs) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), query, args).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// fakeChecks records the links whose checks were scheduled.
type fakeChecks struct {
	mu    sync.Mutex
	links []uuid.UUID
}

func (f *fakeChecks) Schedule(_ context.Context, link uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links = append(f.links, link)
	return nil
}

// fakeSource is Chợ Tốt: profiles with their bios, each profile's
// listings, and three photos every listing shares. down fails every call.
type fakeSource struct {
	mu       sync.Mutex
	bios     map[string]string
	accounts map[string]int64
	ads      map[int64]chotot.Ad
	images   map[string][]byte
	down     bool
	next     int64
}

func newSource(t *testing.T) *fakeSource {
	t.Helper()
	f := &fakeSource{
		bios: map[string]string{}, accounts: map[string]int64{}, ads: map[int64]chotot.Ad{}, images: map[string][]byte{},
		next: 1000,
	}
	for i := range 3 {
		f.images[photo(i)] = mediatest.JPEG(t, 64+i, 48)
	}
	return f
}

func photo(i int) string { return fmt.Sprintf("https://cdn.chotot.com/fake/%d.jpg", i) }

func (f *fakeSource) setBio(profile, bio string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bios[profile] = bio
	if _, ok := f.accounts[profile]; !ok {
		f.accounts[profile] = int64(len(f.accounts) + 1)
	}
}

func (f *fakeSource) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// add puts a listing of profile's on sale and returns its ID, newer than
// any before it.
func (f *fakeSource) add(profile, subject string, price int64, category int) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.ads[f.next] = chotot.Ad{
		ListID: f.next, AccountOID: profile, Category: category, Subject: subject, Body: "Còn mới, ít dùng.",
		Price: price, CategoryName: "Xe đạp", Params: []chotot.Param{}, Images: []string{photo(0), photo(1), photo(2)},
	}
	return f.next
}

func (f *fakeSource) setPrice(listID, price int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.ads[listID]
	a.Price = price
	f.ads[listID] = a
}

func (f *fakeSource) remove(listID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.ads, listID)
}

var errDown = fmt.Errorf("fake: %w", chotot.ErrUnavailable)

func (f *fakeSource) Profile(_ context.Context, profile string) (chotot.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return chotot.Profile{}, errDown
	}
	bio, ok := f.bios[profile]
	if !ok {
		return chotot.Profile{}, chotot.ErrNotFound
	}
	return chotot.Profile{OID: profile, AccountID: f.accounts[profile], Name: "Shop Na", Description: bio}, nil
}

func (f *fakeSource) Ads(_ context.Context, account int64, profile string) ([]chotot.Listed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return nil, errDown
	}
	if f.accounts[profile] != account {
		return nil, errors.New("fake: listed by another account")
	}
	out := []chotot.Listed{}
	for id := range f.ads {
		if a := f.ads[id]; a.AccountOID == profile {
			out = append(out, chotot.Listed{
				ListID: a.ListID, Subject: a.Subject, Price: a.Price, Category: a.Category, CategoryName: a.CategoryName, Thumb: photo(0),
			})
		}
	}
	slices.SortFunc(out, func(a, b chotot.Listed) int { return int(b.ListID - a.ListID) })
	return out, nil
}

func (f *fakeSource) Ad(_ context.Context, listID int64) (chotot.Ad, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return chotot.Ad{}, errDown
	}
	a, ok := f.ads[listID]
	if !ok {
		return chotot.Ad{}, chotot.ErrNotFound
	}
	return a, nil
}

func (f *fakeSource) Image(_ context.Context, url string) (photo []byte, contentType string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.images[url]
	if !ok {
		return nil, "", chotot.ErrBadImage
	}
	return b, "image/jpeg", nil
}

// fakeFill copies the listing as it is under furniture.other, the
// condition guessed. A subject with "nocat" gets no category; one with
// "service" sells no item.
type fakeFill struct{}

func (fakeFill) FillAd(_ context.Context, ad chotot.Ad) (assistsvc.AdFill, error) {
	if strings.Contains(ad.Subject, "service") {
		return assistsvc.AdFill{NoItem: "a service", Guessed: []string{}}, nil
	}
	price := ad.Price
	d := assistdomain.Draft{Title: ad.Subject, Description: ad.Body, Price: &price, CategoryCode: "furniture.other"}
	if strings.Contains(ad.Subject, "nocat") {
		d.CategoryCode = ""
	}
	return assistsvc.AdFill{Draft: d, Guessed: []string{"condition"}}, nil
}

// isCode reports err as the refusal of code.
func isCode(err, target error) bool { return errors.Is(err, target) }

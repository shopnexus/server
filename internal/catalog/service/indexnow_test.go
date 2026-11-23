package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/ingress"

	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/locale"
	"github.com/shopnexus/server/internal/platform/testenv"
	"github.com/shopnexus/server/internal/provider/indexnow"
)

// indexNowStub is an IndexNow endpoint that keeps each submission and
// answers status.
type indexNowStub struct {
	mu     sync.Mutex
	got    []indexnow.Submission
	status atomic.Int32
}

func (s *indexNowStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var sub indexnow.Submission
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.got = append(s.got, sub)
	s.mu.Unlock()
	w.WriteHeader(int(s.status.Load()))
}

func (s *indexNowStub) submissions() []indexnow.Submission {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

// The queue submits each listing once, at its current address, a moment
// after the first one arrived; a listing never on sale is left out. A
// refused submission is not retried, and the queue goes on.
func TestIndexNowQueue(t *testing.T) {
	t.Parallel()
	stub := &indexNowStub{}
	stub.status.Store(http.StatusAccepted)
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	const key = "0123456789abcdef0123456789abcdef"
	e := newEnv(t, func(d *service.Deps) {
		d.IndexNow = service.IndexNowSettings{
			Key: key, Site: "https://shop.example", Client: indexnow.New(srv.Client(), srv.URL), Every: 2 * time.Second,
		}
	})
	ctx := context.Background()
	seller := e.account(t)
	bike := catalogtest.Listing(t, e.db, seller)
	chair := catalogtest.Listing(t, e.db, seller, func(d *domain.ListingInput, _ *domain.Refs) { d.Title = "Ghế gỗ" })
	draft, err := domain.NewListing(uuid.NewV7(), seller, domain.ListingInput{
		Title: "Bàn", Price: 500_000, CategoryCode: "furniture.table", Condition: domain.ConditionUsed,
		CommuneCode: "00004", Photos: []uuid.UUID{uuid.NewV7()},
	}, domain.Refs{CategoryIsLeaf: true, CommuneExists: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = e.listings.Insert(ctx, draft); err != nil {
		t.Fatal(err)
	}
	add := func(id uuid.UUID) {
		t.Helper()
		if _, err := ingress.Object[uuid.UUID, restate.Void](e.ingress, e.prefix+service.IndexNowName, service.IndexNowQueue, "Add").
			Request(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(n int) []indexnow.Submission {
		t.Helper()
		var got []indexnow.Submission
		testenv.Eventually(t, 20*time.Second, func() error {
			if got = stub.submissions(); len(got) < n {
				return fmt.Errorf("%d submissions", len(got))
			}
			return nil
		})
		return got
	}
	slug := func(id uuid.UUID) string {
		t.Helper()
		v, err := e.catalog.Get(ctx, &seller, id, locale.Vietnamese)
		if err != nil {
			t.Fatal(err)
		}
		return "https://shop.example/listings/" + v.Slug
	}

	add(bike)
	add(chair)
	add(bike)
	add(draft.ID())
	first := wait(1)[0]
	want := []string{slug(bike), slug(chair)}
	slices.Sort(want)
	slices.Sort(first.URLs)
	if first.Host != "shop.example" || first.Key != key || first.KeyLocation != "https://shop.example/"+key+".txt" ||
		!slices.Equal(first.URLs, want) {
		t.Fatalf("%+v, want %v", first, want)
	}

	stub.status.Store(http.StatusForbidden)
	add(bike)
	wait(2)
	stub.status.Store(http.StatusOK)
	add(chair)
	if got := wait(3); len(got) != 3 || !slices.Equal(got[2].URLs, []string{slug(chair)}) {
		t.Fatalf("after a refusal: %+v", got)
	}
}

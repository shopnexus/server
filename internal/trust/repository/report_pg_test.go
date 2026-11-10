package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
)

func TestReports(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewReports(db)
	reporter, seller := accounttest.New(t, db), accounttest.New(t, db)
	listing := catalogtest.Listing(t, db, seller)
	at := time.Now().UTC().Truncate(time.Microsecond)
	report := func(listing uuid.UUID, reason domain.Reason, note string, at time.Time) domain.Report {
		t.Helper()
		r, err := domain.NewReport(uuid.NewV7(), reporter, seller, listing, uuid.UUID{}, reason, note, at)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	first, added, err := s.Open(ctx, report(listing, domain.ReasonFake, "", at))
	if err != nil || !added || first.ListingID() != listing {
		t.Fatalf("open: %+v %v %v", first.State(), added, err)
	}
	again, added, err := s.Open(ctx, report(listing, domain.ReasonScam, "Đòi chuyển khoản trước", at.Add(time.Minute)))
	if err != nil || added || again.ID() != first.ID() || again.Reason() != domain.ReasonScam ||
		again.Note() != "Đòi chuyển khoản trước" || !again.CreatedAt().Equal(at) || !again.UpdatedAt().Equal(at.Add(time.Minute)) {
		t.Fatalf("again: %+v %v %v", again.State(), added, err)
	}
	// The account report is a target of its own.
	person, added, err := s.Open(ctx, report(uuid.UUID{}, domain.ReasonScam, "", at))
	if err != nil || !added || person.ID() == first.ID() || person.ListingID() != (uuid.UUID{}) {
		t.Fatalf("account: %+v %v %v", person.State(), added, err)
	}

	for range 2 {
		if err = s.Withdraw(ctx, first.ID(), at.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(ctx, first.ID())
	if err != nil || got.WithdrawnAt() == nil || !got.WithdrawnAt().Equal(at.Add(2*time.Minute)) {
		t.Fatalf("withdrawn: %+v %v", got.State(), err)
	}
	next, added, err := s.Open(ctx, report(listing, domain.ReasonFake, "", at))
	if err != nil || !added || next.ID() == first.ID() {
		t.Fatalf("after a withdrawal: %+v %v %v", next.State(), added, err)
	}
	if _, err = s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

// Two reports of one listing at once leave one open row.
func TestReportsAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewReports(db)
	reporter, seller := accounttest.New(t, db), accounttest.New(t, db)
	listing := catalogtest.Listing(t, db, seller)
	ids := make([]uuid.UUID, 2)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			r, err := domain.NewReport(uuid.NewV7(), reporter, seller, listing, uuid.UUID{}, domain.ReasonFake, "", time.Now())
			if err != nil {
				t.Error(err)
				return
			}
			got, _, err := s.Open(ctx, r)
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = got.ID()
		})
	}
	wg.Wait()
	if ids[0] != ids[1] {
		t.Fatalf("two rows: %v", ids)
	}
}

// An account report keeps its conversation; reporting again replaces it,
// and leaving it out clears it.
func TestReportConversation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewReports(db)
	reporter, seller := accounttest.New(t, db), accounttest.New(t, db)
	lo, hi := min(reporter.String(), seller.String()), max(reporter.String(), seller.String())
	conversation := uuid.NewV7()
	if _, err := db.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, created_at) VALUES (@id, @a, @b, now())`,
		pgx.StrictNamedArgs{"id": conversation, "a": lo, "b": hi}); err != nil {
		t.Fatal(err)
	}
	open := func(c uuid.UUID) domain.Report {
		t.Helper()
		r, err := domain.NewReport(uuid.NewV7(), reporter, seller, uuid.UUID{}, c, domain.ReasonScam, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := s.Open(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := open(conversation); got.ConversationID() != conversation {
		t.Fatalf("open: %+v", got.State())
	}
	again := open(conversation)
	if got, _ := s.Get(ctx, again.ID()); got.ConversationID() != conversation {
		t.Fatalf("get: %+v", got.State())
	}
	if got := open(uuid.UUID{}); got.ConversationID() != (uuid.UUID{}) {
		t.Fatalf("again without one: %+v", got.State())
	}
}

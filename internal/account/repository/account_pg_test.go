package repository_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

func newStore(t *testing.T) repository.Accounts {
	t.Helper()
	return repository.NewAccounts(pgtest.New(t))
}

// seed creates an active account named Linh.
func seed(t *testing.T, s repository.Accounts) uuid.UUID {
	t.Helper()
	a, err := domain.New(uuid.NewV7(), "Linh", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a.ID()
}

// TestPGStore checks that the Postgres store behaves as repository.Accounts
// promises.
func TestPGStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pgNow := time.Now().UTC().Truncate(time.Microsecond)
	fresh := func(t *testing.T, s repository.Accounts) domain.Account {
		t.Helper()
		a, err := domain.New(uuid.NewV7(), "Linh", pgNow)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	t.Run("create then get", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		got, err := s.Get(ctx, a.ID())
		if err != nil || got.ID() != a.ID() || got.DisplayName() != "Linh" || got.Status(pgNow) != domain.StatusActive ||
			got.WriteID() != a.WriteID() || !got.CreatedAt().Equal(pgNow) {
			t.Fatalf("got %+v %v", got, err)
		}
	})

	t.Run("create twice keeps the first", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		again, _ := domain.New(a.ID(), "Other", pgNow)
		if err := s.Create(ctx, again); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Get(ctx, a.ID()); got.DisplayName() != "Linh" {
			t.Fatalf("name %q", got.DisplayName())
		}
	})

	t.Run("get unknown", func(t *testing.T) {
		t.Parallel()
		if _, err := newStore(t).Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("err %v", err)
		}
	})

	t.Run("save sets the write id", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		a.SetDisplayName("Minh", pgNow)
		w := uuid.NewV7()
		if err := s.Save(ctx, &a, w); err != nil || a.WriteID() != w {
			t.Fatalf("save: %v, write id %s", err, a.WriteID())
		}
		if got, _ := s.Get(ctx, a.ID()); got.DisplayName() != "Minh" || got.WriteID() != w {
			t.Fatalf("got %+v", got)
		}
	})

	// Save leaves the account carrying the write ID it wrote, so saving it
	// again succeeds.
	t.Run("save twice", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		for _, name := range []string{"One", "Two"} {
			if err := a.SetDisplayName(name, pgNow); err != nil {
				t.Fatal(err)
			}
			if err := s.Save(ctx, &a, uuid.NewV7()); err != nil {
				t.Fatalf("save %s: %v", name, err)
			}
		}
	})

	t.Run("a stale write conflicts", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		b, _ := s.Get(ctx, a.ID())
		a.SetDisplayName("First", pgNow)
		if err := s.Save(ctx, &a, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
		b.SetDisplayName("Second", pgNow)
		if err := s.Save(ctx, &b, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale save: %v", err)
		}
	})

	t.Run("a retried write with the same id succeeds", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		a.SetDisplayName("Minh", pgNow)
		retry := a
		w := uuid.NewV7()
		if err := s.Save(ctx, &a, w); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &retry, w); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		for range 2 {
			if err := s.Delete(ctx, a.ID()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Get(ctx, a.ID()); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("after delete: %v", err)
		}
	})
}

func TestSaveAuditedWritesBothOrNeither(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	ctx := context.Background()
	id := seed(t, s)
	entry := audit.Entry{Actor: audit.ByCLI("test"), Action: audit.AccountSuspend, Target: audit.Account(id), At: now}
	count := func() int {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM staff_actions WHERE target_id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	a, _ := s.Get(ctx, id)
	stale := a
	if err := a.Suspend("spam", nil, now); err != nil {
		t.Fatal(err)
	}
	w := uuid.NewV7()
	if err := s.SaveAudited(ctx, &a, w, entry); err != nil || a.WriteID() != w || count() != 1 {
		t.Fatalf("%v, write id %v, rows %d", err, a.WriteID(), count())
	}
	if err := s.SaveAudited(ctx, &stale, uuid.NewV7(), entry); !errors.Is(err, repository.ErrConflict) || count() != 1 {
		t.Fatalf("stale save: %v, rows %d", err, count())
	}
}

func TestListNewestFirst(t *testing.T) {
	t.Parallel()
	s := repository.NewAccounts(pgtest.New(t))
	ctx := context.Background()
	first, second := seed(t, s), seed(t, s)
	all, err := s.List(ctx, page.Request{Limit: 10})
	if err != nil || len(all) != 2 || all[0].ID() != second || all[1].ID() != first {
		t.Fatalf("%v %v", all, err)
	}
	rest, _ := s.List(ctx, page.Request{Cursor: second, Limit: 10})
	if len(rest) != 1 || rest[0].ID() != first {
		t.Fatalf("after the cursor: %v", rest)
	}
}

func eventsOf(t *testing.T, db *pgxpool.Pool, subject uuid.UUID) []string {
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

// Save and SaveAudited write their events with the change; a stale write
// writes none.
func TestSaveWritesItsEvents(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	ctx := context.Background()
	id := seed(t, s)
	a, _ := s.Get(ctx, id)
	stale := a
	ev := func(k events.Kind) events.Event {
		return events.Event{ID: uuid.NewV7(), Kind: k, SubjectID: id, OccurredAt: now}
	}
	if err := a.SetDisplayName("Minh", now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &a, uuid.NewV7(), ev(events.AccountRenamed)); err != nil {
		t.Fatal(err)
	}
	if err := a.Suspend("spam", nil, now); err != nil {
		t.Fatal(err)
	}
	entry := audit.Entry{Actor: audit.ByCLI("test"), Action: audit.AccountSuspend, Target: audit.Account(id), At: now}
	if err := s.SaveAudited(ctx, &a, uuid.NewV7(), entry, ev(events.AccountSuspended)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &stale, uuid.NewV7(), ev(events.AccountRenamed)); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if got := eventsOf(t, db, id); !slices.Equal(got, []string{"account.renamed", "account.suspended"}) {
		t.Fatalf("%v", got)
	}
}

func TestBlocks(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	a, b, c := seed(t, s), seed(t, s), seed(t, s)
	for range 2 {
		if err := s.Block(ctx, uuid.NewV7(), a, b, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Block(ctx, uuid.NewV7(), a, c, now); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]uuid.UUID{{a, b}, {b, a}} {
		if blocked, err := s.Blocked(ctx, pair[0], pair[1]); err != nil || !blocked {
			t.Fatalf("%v: %v %v", pair, blocked, err)
		}
	}
	if blocked, err := s.Blocked(ctx, b, c); err != nil || blocked {
		t.Fatalf("b, c: %v %v", blocked, err)
	}
	got, err := s.ListBlocks(ctx, a, page.Request{})
	if err != nil || len(got) != 2 || got[0].AccountID != c || got[1].AccountID != b {
		t.Fatalf("list: %+v %v", got, err)
	}
	if err = s.Unblock(ctx, a, b); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := s.Blocked(ctx, b, a); blocked {
		t.Fatal("still blocked")
	}
}

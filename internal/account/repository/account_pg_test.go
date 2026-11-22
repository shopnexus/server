package repository_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/events"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
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

// TestPGAccounts checks that the Postgres store behaves as repository.Accounts
// promises.
func TestPGAccounts(t *testing.T) {
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
		if err := a.Edit(new("Minh"), new("Đồ cũ còn tốt"), pgNow); err != nil {
			t.Fatal(err)
		}
		a.SetAvatar("media/a.jpg", pgNow)
		w := uuid.NewV7()
		if err := s.Save(ctx, &a, w, nil); err != nil || a.WriteID() != w {
			t.Fatalf("save: %v, write id %s", err, a.WriteID())
		}
		got, _ := s.Get(ctx, a.ID())
		if got.DisplayName() != "Minh" || got.Bio() != "Đồ cũ còn tốt" || got.AvatarKey() != "media/a.jpg" || got.WriteID() != w {
			t.Fatalf("got %+v", got)
		}
		if err := a.Edit(nil, new(""), pgNow); err != nil {
			t.Fatal(err)
		}
		a.SetAvatar("", pgNow)
		if err := s.Save(ctx, &a, uuid.NewV7(), nil); err != nil {
			t.Fatal(err)
		}
		if got, _ = s.Get(ctx, a.ID()); got.Bio() != "" || got.AvatarKey() != "" {
			t.Fatalf("not cleared: %+v", got)
		}
	})

	// Save leaves the account carrying the write ID it wrote, so saving it
	// again succeeds.
	t.Run("save twice", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		for _, name := range []string{"One", "Two"} {
			if err := a.Edit(&name, nil, pgNow); err != nil {
				t.Fatal(err)
			}
			if err := s.Save(ctx, &a, uuid.NewV7(), nil); err != nil {
				t.Fatalf("save %s: %v", name, err)
			}
		}
	})

	t.Run("a stale write conflicts", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		b, _ := s.Get(ctx, a.ID())
		a.Edit(new("First"), nil, pgNow)
		if err := s.Save(ctx, &a, uuid.NewV7(), nil); err != nil {
			t.Fatal(err)
		}
		b.Edit(new("Second"), nil, pgNow)
		if err := s.Save(ctx, &b, uuid.NewV7(), nil); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale save: %v", err)
		}
	})

	t.Run("a retried write with the same id succeeds", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		a := fresh(t, s)
		a.Edit(new("Minh"), nil, pgNow)
		retry := a
		w := uuid.NewV7()
		if err := s.Save(ctx, &a, w, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &retry, w, nil); err != nil {
			t.Fatalf("retry: %v", err)
		}
	})
}

func TestSaveWritesItsAuditEntryOrNothing(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	ctx := context.Background()
	id := seed(t, s)
	entry := audit.Entry{Actor: audit.ByCLI("test"), Action: audit.AccountSuspend, Target: audit.Account(id), At: now}
	count := func() int {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM console_actions WHERE target_id = @id`, pgx.StrictNamedArgs{"id": id}).Scan(&n); err != nil {
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
	if err := s.Save(ctx, &a, w, &entry); err != nil || a.WriteID() != w || count() != 1 {
		t.Fatalf("%v, write id %v, rows %d", err, a.WriteID(), count())
	}
	if err := s.Save(ctx, &stale, uuid.NewV7(), &entry); !errors.Is(err, repository.ErrConflict) || count() != 1 {
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

// ListSuspended lists the suspensions in force, the one that ends soonest
// first and one without an end last, and leaves out a lapsed one and an
// active account; CountSuspended counts the same accounts.
func TestListSuspended(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	suspend := func(until *time.Time) uuid.UUID {
		t.Helper()
		id := seed(t, s)
		a, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Suspend("spam", until, now); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(ctx, &a, uuid.NewV7(), nil); err != nil {
			t.Fatal(err)
		}
		return id
	}
	later, soon, lapsed := now.Add(2*time.Hour), now.Add(time.Hour), now.Add(10*time.Minute)
	noEnd, second, first := suspend(nil), suspend(&later), suspend(&soon)
	suspend(&lapsed)
	seed(t, s)
	at := now.Add(30 * time.Minute)

	got, err := s.ListSuspended(ctx, at, page.Request{Limit: 10})
	ids := make([]uuid.UUID, 0, len(got))
	for _, a := range got {
		ids = append(ids, a.ID())
	}
	if err != nil || !slices.Equal(ids, []uuid.UUID{first, second, noEnd}) {
		t.Fatalf("%v %v", ids, err)
	}
	rest, err := s.ListSuspended(ctx, at, page.Request{Cursor: second, Limit: 10})
	if err != nil || len(rest) != 1 || rest[0].ID() != noEnd {
		t.Fatalf("after the cursor: %v %v", rest, err)
	}
	if n, err := s.CountSuspended(ctx, at); err != nil || n != 3 {
		t.Fatalf("count: %d %v", n, err)
	}
	byStatus, countErr := s.CountByStatus(ctx, at, time.Time{})
	if want := map[domain.Status]int{domain.StatusSuspended: 3, domain.StatusActive: 2}; countErr != nil || !maps.Equal(byStatus, want) {
		t.Fatalf("by status: %v %v", byStatus, countErr)
	}
	if byStatus, countErr = s.CountByStatus(ctx, at, now.Add(time.Second)); countErr != nil || len(byStatus) != 0 {
		t.Fatalf("created since a later time: %v %v", byStatus, countErr)
	}
}

// GetMany returns the accounts it finds and leaves an unknown ID out.
func TestGetMany(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	a, b := seed(t, s), seed(t, s)
	got, err := s.GetMany(ctx, []uuid.UUID{a, b, uuid.NewV7()})
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	ids := []uuid.UUID{got[0].ID(), got[1].ID()}
	if !slices.Contains(ids, a) || !slices.Contains(ids, b) || got[0].DisplayName() != "Linh" {
		t.Fatalf("%v", got)
	}
}

// Save writes its events with the change; a stale write writes none.
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
	if err := a.Edit(new("Minh"), nil, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &a, uuid.NewV7(), nil, ev(events.AccountUpdated)); err != nil {
		t.Fatal(err)
	}
	if err := a.Suspend("spam", nil, now); err != nil {
		t.Fatal(err)
	}
	entry := audit.Entry{Actor: audit.ByCLI("test"), Action: audit.AccountSuspend, Target: audit.Account(id), At: now}
	if err := s.Save(ctx, &a, uuid.NewV7(), &entry, ev(events.AccountSuspended)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, &stale, uuid.NewV7(), nil, ev(events.AccountUpdated)); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if got := pgtest.EventKinds(t, db, id); !slices.Equal(got, []string{"account.updated", "account.suspended"}) {
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

func TestTouchActive(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	id := seed(t, s)
	before, err := s.Get(ctx, id)
	if err != nil || before.LastActiveAt() != nil {
		t.Fatalf("%v %v", before.LastActiveAt(), err)
	}
	at := now.Add(time.Hour)
	for _, c := range []struct {
		touch, want time.Time
	}{
		{at, at},
		{at.Add(10 * time.Second), at}, // under 30 s later: not written
		{at.Add(31 * time.Second), at.Add(31 * time.Second)},
	} {
		if err = s.TouchActive(ctx, id, c.touch); err != nil {
			t.Fatal(err)
		}
		a, getErr := s.Get(ctx, id)
		if getErr != nil || a.LastActiveAt() == nil || !a.LastActiveAt().Equal(c.want) {
			t.Fatalf("touch %s: %v %v", c.touch, a.LastActiveAt(), getErr)
		}
		if a.WriteID() != before.WriteID() {
			t.Fatal("TouchActive changed write_id")
		}
	}
}

// count is how many rows q finds for id.
func count(t *testing.T, db *pgxpool.Pool, q string, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), q, pgx.StrictNamedArgs{"id": id}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestErase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	me, other := seed(t, s), seed(t, s)
	for _, q := range []string{
		`INSERT INTO credentials (account_id, write_id) VALUES (@id, @id)`,
		`INSERT INTO identities (id, account_id, kind, identifier) VALUES (uuidv7(), @id::uuid, 'phone', '+8499' || substr(md5(@id::text), 1, 7))`,
		`INSERT INTO identities (id, account_id, kind, identifier) VALUES (uuidv7(), @id::uuid, 'email', @id::text || '@example.com')`,
		// A sign-in code to the account's email, an add-email code it asked
		// for, and the email's send quota.
		`INSERT INTO otp_challenges (id, purpose, kind, destination, code_hash, ip, expires_at, created_at, write_id)
VALUES (uuidv7(), 'sign_in', 'email', @id::text || '@example.com', '\x00', '127.0.0.1', now() + interval '5 minutes', now(), uuidv7())`,
		`INSERT INTO otp_challenges (id, purpose, kind, destination, account_id, code_hash, ip, expires_at, created_at, write_id)
VALUES (uuidv7(), 'add_identity', 'email', 'other-' || @id::text || '@example.com', @id::uuid, '\x00', '127.0.0.1', now() + interval '5 minutes', now(), uuidv7())`,
		`INSERT INTO otp_quotas (subject, sent_at, write_id) VALUES ('dest:' || @id::text || '@example.com', ARRAY[now()], uuidv7())`,
		`INSERT INTO addresses (id, account_id, recipient_name, phone, street, commune_code, kind, is_default, request_key, write_id, created_at)
VALUES (uuidv7(), @id, 'Linh', '0912345678', '1 Lê Lợi', (SELECT code FROM communes ORDER BY code LIMIT 1), 'home', true, gen_random_uuid(), uuidv7(), now())`,
		`INSERT INTO sessions (id, account_id, refresh_hash, auth_time, created_at, last_used_at, expires_at, write_id)
VALUES (uuidv7(), @id, decode(md5(random()::text), 'hex'), now(), current_timestamp, transaction_timestamp(), now() + interval '30 days', uuidv7())`,
	} {
		if _, err := db.Exec(ctx, q, pgx.StrictNamedArgs{"id": me}); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Block(ctx, uuid.NewV7(), other, me, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO account_follows (id, follower_id, followed_id, created_at) VALUES (uuidv7(), @id, @other, now())`,
		pgx.StrictNamedArgs{"id": me, "other": other}); err != nil {
		t.Fatal(err)
	}

	a, err := s.Get(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Delete(now); err != nil {
		t.Fatal(err)
	}
	ev := events.Event{ID: uuid.NewV7(), Kind: events.AccountDeleted, SubjectID: me, OccurredAt: now}
	if err = s.Erase(ctx, &a, uuid.NewV7(), ev); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, me)
	if err != nil || got.StoredStatus() != domain.StatusDeleted || got.DeletedAt() == nil || got.DisplayName() != "" {
		t.Fatalf("stored: %+v %v", got, err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM credentials WHERE account_id = @id`:                                                 0,
		`SELECT count(*) FROM identities WHERE account_id = @id`:                                                  0,
		`SELECT count(*) FROM addresses WHERE account_id = @id`:                                                   0,
		`SELECT count(*) FROM account_blocks WHERE blocker_id = @id OR blocked_id = @id`:                          0,
		`SELECT count(*) FROM account_follows WHERE follower_id = @id OR followed_id = @id`:                       0,
		`SELECT count(*) FROM sessions WHERE account_id = @id AND revoked_at IS NULL`:                             0,
		`SELECT count(*) FROM events WHERE subject_id = @id AND kind = 'account.deleted'`:                         1,
		`SELECT count(*) FROM otp_challenges WHERE account_id = @id OR destination = @id::text || '@example.com'`: 0,
		`SELECT count(*) FROM otp_quotas WHERE subject = 'dest:' || @id::text || '@example.com'`:                  0,
	} {
		if n := count(t, db, q, me); n != want {
			t.Errorf("%s: %d, want %d", q, n, want)
		}
	}

	stale := got
	stale.Saved(uuid.NewV7())
	if err = s.Erase(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a stale erase: %v", err)
	}
}

// BlockedWith lists the accounts on either side of a person's blocks.
func TestBlockedWith(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me, blockedByMe, blocksMe, other := seed(t, s), seed(t, s), seed(t, s), seed(t, s)
	if err := s.Block(ctx, uuid.NewV7(), me, blockedByMe, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Block(ctx, uuid.NewV7(), blocksMe, me, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Block(ctx, uuid.NewV7(), other, blocksMe, now); err != nil { // a block not mine
		t.Fatal(err)
	}
	got, err := s.BlockedWith(ctx, me)
	byID := func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }
	slices.SortFunc(got, byID)
	want := []uuid.UUID{blockedByMe, blocksMe}
	slices.SortFunc(want, byID)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("%v %v", got, err)
	}
}

// CountWithPhone counts the accounts with a phone and leaves out a deleted
// one that still has its identity.
func TestCountWithPhone(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewAccounts(db)
	ctx := context.Background()
	accounttest.WithPhone(t, db)
	gone := accounttest.WithPhone(t, db)
	accounttest.New(t, db)
	if n, err := s.CountWithPhone(ctx); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	if _, err := db.Exec(ctx, `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountWithPhone(ctx); err != nil || n != 1 {
		t.Fatalf("after a deletion: %d %v", n, err)
	}
}

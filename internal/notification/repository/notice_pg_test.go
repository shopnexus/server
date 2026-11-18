package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/notification/repository"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server/internal/platform/testenv"
)

var now = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

func notice(to uuid.UUID, key string, amount int64, at time.Time) domain.Notice {
	return domain.Notice{Recipient: to, Type: domain.TypeOffer, CollapseKey: key, Params: domain.Params{Amount: amount}, At: at}
}

func TestCollapse(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me := accounttest.New(t, db)
	expired := now.Add(-domain.Retention)
	first, _, err := s.Add(ctx, uuid.NewV7(), notice(me, "deal:1", 100, now), expired)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.Add(ctx, uuid.NewV7(), notice(me, "deal:1", 200, now.Add(time.Minute)), expired)
	if err != nil || second != first {
		t.Fatalf("an unread notice collapses: %v %v", second, err)
	}
	var late bool
	if _, late, err = s.Add(ctx, uuid.NewV7(), notice(me, "deal:1", 50, now.Add(-time.Minute)), expired); err != nil || !late {
		t.Fatalf("an older event is late: %v %v", late, err)
	}
	rows, err := s.List(ctx, me, page.Request{})
	if err != nil || len(rows) != 1 || rows[0].Params.Amount != 200 {
		t.Fatalf("an older event keeps the newer content: %+v %v", rows, err)
	}
	if err = s.Read(ctx, me, first, now); err != nil {
		t.Fatal(err)
	}
	third, _, err := s.Add(ctx, uuid.NewV7(), notice(me, "deal:1", 300, now.Add(2*time.Minute)), expired)
	if err != nil || third == first {
		t.Fatalf("a read notice starts a new one: %v %v", third, err)
	}
	if n, _ := s.Unread(ctx, me); n != 1 {
		t.Fatalf("unread %d", n)
	}
	if _, err = s.ReadAll(ctx, me, now); err != nil {
		t.Fatal(err)
	}
	if _, late, err = s.Add(ctx, uuid.NewV7(), notice(me, "deal:1", 250, now.Add(time.Minute)), expired); err != nil || !late {
		t.Fatalf("an event older than a read notice is late: %v %v", late, err)
	}
	if n, _ := s.Unread(ctx, me); n != 0 {
		t.Fatalf("an event older than a read notice starts a new one: unread %d", n)
	}
}

func TestAddOnceAndPrune(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	event := uuid.NewV7()
	review := domain.Notice{Recipient: me, Type: domain.TypeReview, At: now}
	a, _, err := s.Add(ctx, event, review, now.Add(-domain.Retention))
	if err != nil {
		t.Fatal(err)
	}
	if b, _, again := s.Add(ctx, event, review, now.Add(-domain.Retention)); again != nil || b != a {
		t.Fatalf("a redelivered event: %v %v", b, again)
	}
	old := now.Add(-100 * 24 * time.Hour)
	for _, who := range []uuid.UUID{me, other} {
		if _, _, err = s.Add(ctx, uuid.NewV7(), domain.Notice{Recipient: who, Type: domain.TypeReview, At: old},
			old.Add(-domain.Retention)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = s.Add(ctx, uuid.NewV7(), review, now.Add(-domain.Retention)); err != nil {
		t.Fatal(err)
	}
	mine, _ := s.List(ctx, me, page.Request{})
	theirs, _ := s.List(ctx, other, page.Request{})
	if len(mine) != 2 || len(theirs) != 1 {
		t.Fatalf("prune deletes only the recipient's old notices: %d %d", len(mine), len(theirs))
	}
}

func TestReadAndList(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me, stranger := accounttest.New(t, db), accounttest.New(t, db)
	ids := make([]uuid.UUID, 0, 3)
	for i := range 3 {
		id, _, err := s.Add(ctx, uuid.NewV7(), notice(me, "", int64(i), now.Add(time.Duration(i)*time.Minute)), now.Add(-domain.Retention))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows, err := s.List(ctx, me, page.Request{Limit: 2})
	if err != nil || len(rows) != 3 || rows[0].ID != ids[2] {
		t.Fatalf("newest first, one extra row: %+v %v", rows, err)
	}
	next, err := s.List(ctx, me, page.Request{Limit: 2, Cursor: rows[1].ID})
	if err != nil || len(next) != 1 || next[0].ID != ids[0] {
		t.Fatalf("second page: %+v %v", next, err)
	}
	if err = s.Read(ctx, stranger, ids[0], now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if _, err = s.ReadAll(ctx, me, now); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Unread(ctx, me); n != 0 {
		t.Fatalf("after read-all: %d", n)
	}
}

func TestTokens(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me := accounttest.New(t, db)
	session := uuid.NewV7()
	for _, tok := range []string{"a", "b"} {
		if err := s.PutToken(ctx, session, me, tok, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutToken(ctx, uuid.NewV7(), me, "c", now); err != nil {
		t.Fatal(err)
	}
	got, err := s.Tokens(ctx, me)
	if err != nil || len(got) != 2 {
		t.Fatalf("one token per session: %+v %v", got, err)
	}
	if err = s.DeleteToken(ctx, session); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Tokens(ctx, me); len(got) != 1 || got[0].Token != "c" {
		t.Fatalf("after delete: %+v", got)
	}
}

// A phone signed in to a second account keeps its token, which moves to
// the new session, so the first account's pushes stop reaching it.
func TestTokenMovesToTheNewSession(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	first, second := accounttest.New(t, db), accounttest.New(t, db)
	if err := s.PutToken(ctx, uuid.NewV7(), first, "phone", now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutToken(ctx, uuid.NewV7(), second, "phone", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Tokens(ctx, first); err != nil || len(got) != 0 {
		t.Fatalf("the first account still has the phone: %+v %v", got, err)
	}
	if got, err := s.Tokens(ctx, second); err != nil || len(got) != 1 {
		t.Fatalf("the second account: %+v %v", got, err)
	}
}

func TestForget(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	for _, to := range []uuid.UUID{me, other} {
		if _, _, err := s.Add(ctx, uuid.NewV7(), notice(to, "", 1, now), now.Add(-domain.Retention)); err != nil {
			t.Fatal(err)
		}
		if err := s.PutToken(ctx, uuid.NewV7(), to, "token-"+to.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := s.Forget(ctx, me); err != nil {
			t.Fatal(err)
		}
	}
	if rows, _ := s.List(ctx, me, page.Request{}); len(rows) != 0 {
		t.Fatalf("notices left: %d", len(rows))
	}
	if tokens, _ := s.Tokens(ctx, me); len(tokens) != 0 {
		t.Fatalf("tokens left: %d", len(tokens))
	}
	if rows, _ := s.List(ctx, other, page.Request{}); len(rows) != 1 {
		t.Fatalf("the other account's notices: %d", len(rows))
	}
}

// ReadAll returns exactly the notices it marked read.
func TestReadAllReturnsItsIDs(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me := accounttest.New(t, db)
	expired := now.Add(-domain.Retention)
	read, _, err := s.Add(ctx, uuid.NewV7(), notice(me, "deal:1", 100, now), expired)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Read(ctx, me, read, now); err != nil {
		t.Fatal(err)
	}
	unread, _, err := s.Add(ctx, uuid.NewV7(), notice(me, "deal:2", 100, now), expired)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := s.ReadAll(ctx, me, now)
	if err != nil || len(ids) != 1 || ids[0] != unread {
		t.Fatalf("read all: %v %v", ids, err)
	}
	if ids, err = s.ReadAll(ctx, me, now); err != nil || len(ids) != 0 || ids == nil {
		t.Fatalf("again: %#v %v", ids, err)
	}
}

// MarkUnread makes the caller's read notices unread, one per collapse key:
// the newest, and none for a key that has an unread notice.
func TestMarkUnreadCollapse(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me, other := accounttest.New(t, db), accounttest.New(t, db)
	expired := now.Add(-domain.Retention)
	add := func(to uuid.UUID, key string, at time.Time) uuid.UUID {
		t.Helper()
		id, _, err := s.Add(ctx, uuid.NewV7(), notice(to, key, 100, at), expired)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Read(ctx, to, id, at); err != nil {
			t.Fatal(err)
		}
		return id
	}
	older, newer := add(me, "deal:1", now), add(me, "deal:1", now.Add(time.Minute))
	keyless := add(me, "", now)
	covered := add(me, "deal:2", now)
	if _, _, err := s.Add(ctx, uuid.NewV7(), notice(me, "deal:2", 200, now.Add(time.Minute)), expired); err != nil {
		t.Fatal(err)
	}
	theirs := add(other, "", now)
	if err := s.MarkUnread(ctx, me, []uuid.UUID{older, newer, keyless, covered, theirs, uuid.NewV7()}); err != nil {
		t.Fatal(err)
	}
	unread := func(account uuid.UUID) map[uuid.UUID]bool {
		t.Helper()
		rows, err := s.List(ctx, account, page.Request{})
		if err != nil {
			t.Fatal(err)
		}
		out := map[uuid.UUID]bool{}
		for _, r := range rows {
			out[r.ID] = !r.Read
		}
		return out
	}
	got := unread(me)
	if got[older] || !got[newer] || !got[keyless] || got[covered] {
		t.Fatalf("me: %v", got)
	}
	if unread(other)[theirs] {
		t.Fatal("someone else's notice changed")
	}
	if err := s.MarkUnread(ctx, me, []uuid.UUID{newer}); err != nil {
		t.Fatalf("an unread notice again: %v", err)
	}
}

// When a newer unread notice with the key commits while Add waits on it,
// Add keeps it, reports late, and logs no update for the recipient, since
// nothing changed.
func TestAddLosingTheRaceLogsNoUpdate(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me := accounttest.New(t, db)
	newer := uuid.NewV7()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO notifications (id, account_id, notice_type, collapse_key, params, event_id, created_at, bumped_at)
VALUES (@id, @account, 'offer', 'deal:1', '{}', @event, @at, @at)`,
		pgx.StrictNamedArgs{"id": newer, "account": me, "event": uuid.NewV7(), "at": now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	type added struct {
		id   uuid.UUID
		late bool
		err  error
	}
	done := make(chan added, 1)
	go func() {
		id, late, addErr := s.Add(ctx, uuid.NewV7(), notice(me, "deal:1", 100, now), now.Add(-domain.Retention))
		done <- added{id, late, addErr}
	}()
	// Add's insert waits on the uncommitted notice's key.
	testenv.Eventually(t, 5*time.Second, func() error {
		var waiting int
		if readErr := db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); readErr != nil {
			return readErr
		}
		if waiting == 0 {
			return errors.New("Add is not waiting yet")
		}
		return nil
	})
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.id != newer || !got.late {
		t.Fatalf("Add: %+v, want the newer notice %s, late", got, newer)
	}
	var updates int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM account_updates WHERE account_id = @account`,
		pgx.StrictNamedArgs{"account": me}).Scan(&updates); err != nil || updates != 0 {
		t.Fatalf("updates %d: %v", updates, err)
	}
}

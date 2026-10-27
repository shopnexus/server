package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

var now = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

func account(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

func notice(to uuid.UUID, key string, amount int64, at time.Time) domain.Notice {
	return domain.Notice{Recipient: to, Type: domain.TypeOffer, CollapseKey: key, Params: map[string]any{"amount": amount}, At: at}
}

func TestCollapse(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	s := repository.NewNotices(db)
	me := account(t, db)
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
	if err != nil || len(rows) != 1 || rows[0].Params["amount"] != float64(200) {
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
	if err = s.ReadAll(ctx, me, now); err != nil {
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
	me, other := account(t, db), account(t, db)
	event := uuid.NewV7()
	review := domain.Notice{Recipient: me, Type: domain.TypeReview, Params: map[string]any{}, At: now}
	a, _, err := s.Add(ctx, event, review, now.Add(-domain.Retention))
	if err != nil {
		t.Fatal(err)
	}
	if b, _, again := s.Add(ctx, event, review, now.Add(-domain.Retention)); again != nil || b != a {
		t.Fatalf("a redelivered event: %v %v", b, again)
	}
	old := now.Add(-100 * 24 * time.Hour)
	for _, who := range []uuid.UUID{me, other} {
		if _, _, err = s.Add(ctx, uuid.NewV7(), domain.Notice{Recipient: who, Type: domain.TypeReview, Params: map[string]any{}, At: old},
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
	me, stranger := account(t, db), account(t, db)
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
	if err = s.ReadAll(ctx, me, now); err != nil {
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
	me := account(t, db)
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
	first, second := account(t, db), account(t, db)
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

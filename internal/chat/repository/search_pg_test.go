package repository_test

import (
	"context"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	catalogdomain "github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func terms(t *testing.T, s repository.Chats, q string) repository.Terms {
	t.Helper()
	toks, err := s.Tokens(context.Background(), q)
	if err != nil || len(toks) == 0 {
		t.Fatalf("tokens of %q: %v %v", q, toks, err)
	}
	return repository.Terms{Whole: toks[:len(toks)-1], Prefix: toks[len(toks)-1]}
}

func TestTokens(t *testing.T) {
	t.Parallel()
	s := repository.NewChats(pgtest.New(t))
	for q, want := range map[string][]string{
		"Giày ĐỒ cũ, SHIP!": {"giay", "do", "cu", "ship"},
		"  !!! ,, ":         {},
		"a.b+c":             {"a", "b", "c"},
	} {
		got, err := s.Tokens(context.Background(), q)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: %v %v", q, got, err)
		}
	}
}

func messageIDs(rows []repository.MessageHitRow) []uuid.UUID {
	out := make([]uuid.UUID, len(rows))
	for i := range rows {
		out[i] = rows[i].Message.ID()
	}
	return out
}

// Words fold case and accents, every word but the last matches whole, the
// last as a prefix; only sent messages of the viewer's listed
// conversations, archived ones too, newest first, paged by cursor.
func TestSearchMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	c, a, b := pair(t, db, s)
	first := send(t, s, c, b, "Giày SHIP nhanh không bạn")
	second := send(t, s, c, a, "ship nhé, đồ còn mới")
	send(t, s, c, b, "nhanh lên")
	photo := send(t, s, c, b, "ship nhanh", uuid.NewV7()) // processing: only its sender sees it
	other, x, _ := pair(t, db, s)
	send(t, s, other, x, "ship nhanh")
	if err := s.SetArchived(ctx, c.ID, a, new(time.Now())); err != nil {
		t.Fatal(err)
	}

	got, err := s.SearchMessages(ctx, a, terms(t, s, "ship NH"), page.Request{})
	if err != nil || !slices.Equal(messageIDs(got), []uuid.UUID{second.ID(), first.ID()}) || got[0].Other != b {
		t.Fatalf("ship nh: %v %v", messageIDs(got), err)
	}
	if got, _ = s.SearchMessages(ctx, a, terms(t, s, "giay"), page.Request{}); !slices.Equal(messageIDs(got), []uuid.UUID{first.ID()}) {
		t.Fatalf("giay: %v", messageIDs(got))
	}
	if got, _ = s.SearchMessages(ctx, a, terms(t, s, "Đồ"), page.Request{}); !slices.Equal(messageIDs(got), []uuid.UUID{second.ID()}) {
		t.Fatalf("đồ: %v", messageIDs(got))
	}
	if got, _ = s.SearchMessages(ctx, a, terms(t, s, "shi nhanh"), page.Request{}); len(got) != 0 {
		t.Fatalf("a middle word matches whole: %v", messageIDs(got))
	}
	if got, _ = s.SearchMessages(ctx, a, terms(t, s, "ship"), page.Request{Limit: 1}); len(got) != 2 ||
		got[0].Message.ID() != second.ID() {
		t.Fatalf("first page reads one more: %v", messageIDs(got))
	}
	if got, _ = s.SearchMessages(ctx, a, terms(t, s, "ship"), page.Request{Cursor: second.ID()}); !slices.Equal(messageIDs(got), []uuid.UUID{first.ID()}) {
		t.Fatalf("next page: %v", messageIDs(got))
	}
	if slices.Contains(messageIDs(got), photo.ID()) {
		t.Fatal("a processing message matched")
	}
	if _, err = db.Exec(ctx, `INSERT INTO account_blocks (id, blocker_id, blocked_id, created_at) VALUES (@id, @a, @b, now())`,
		pgx.StrictNamedArgs{"id": uuid.NewV7(), "a": a, "b": b}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.SearchMessages(ctx, a, terms(t, s, "ship"), page.Request{}); len(got) != 0 {
		t.Fatalf("a blocked pair: %v", messageIDs(got))
	}
}

// A thread matches by the other member's name or by the title of a listing
// a message names; a deleted person's empty name matches nothing.
func TestSearchThreads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	viewer := accounttest.New(t, db)
	byName, hanh := pairWith(t, db, s, viewer)
	byTitle, seller := pairWith(t, db, s, viewer)
	deleted, gone := pairWith(t, db, s, viewer)
	for id, name := range map[uuid.UUID]string{hanh: "Ngọc Hân", seller: "Shop Đồ Cũ", gone: "Trần Hân"} {
		if _, err := db.Exec(ctx, `UPDATE accounts SET display_name = @n WHERE id = @id`, pgx.StrictNamedArgs{"n": name, "id": id}); err != nil {
			t.Fatal(err)
		}
	}
	send(t, s, byName, hanh, "Chào")
	listing := catalogtest.Listing(t, db, seller, func(d *catalogdomain.ListingInput, _ *catalogdomain.Refs) { d.Title = "Giày Nike Air cũ" })
	m, err := domain.NewMessage(uuid.NewV7(), byTitle.ID, viewer, "Còn không?", nil, &listing, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AddMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	send(t, s, deleted, gone, "Alo")
	if _, err = db.Exec(ctx, `UPDATE accounts SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL,
    avatar_key = NULL WHERE id = @id`, pgx.StrictNamedArgs{"id": gone}); err != nil {
		t.Fatal(err)
	}
	ids := func(rows []repository.InboxRow) []uuid.UUID {
		out := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			out[i] = r.Conversation.ID
		}
		return out
	}
	for q, want := range map[string][]uuid.UUID{
		"ngoc ha":   {byName.ID},
		"HAN":       {byName.ID},
		"giay nik":  {byTitle.ID},
		"do cu":     {byTitle.ID},
		"nike shop": {},
	} {
		got, serr := s.SearchThreads(ctx, viewer, terms(t, s, q), 20)
		if serr != nil || !slices.Equal(ids(got), want) {
			t.Errorf("%q: %v %v", q, ids(got), serr)
		}
	}
	if err = s.SetArchived(ctx, byName.ID, viewer, new(time.Now())); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads(ctx, viewer, terms(t, s, "han"), 20)
	if err != nil || len(got) != 1 || !got[0].Archived || got[0].Other != hanh || got[0].Unread != 1 {
		t.Fatalf("archived thread: %+v %v", got, err)
	}
}

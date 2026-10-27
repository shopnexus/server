package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

func newAccount(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO accounts (id, write_id) VALUES (@id, @id)`, pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
	return id
}

// pair opens the conversation of two new accounts.
func pair(t *testing.T, db *pgxpool.Pool, s repository.Chats) (c domain.Conversation, a, b uuid.UUID) {
	t.Helper()
	a, b = newAccount(t, db), newAccount(t, db)
	c, err := s.OpenPair(context.Background(), domain.NewConversation(uuid.NewV7(), a, b, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return c, a, b
}

// send adds a message of sender's to c.
func send(t *testing.T, s repository.Chats, c domain.Conversation, sender uuid.UUID, body string, photos ...uuid.UUID) domain.Message {
	t.Helper()
	m, err := domain.NewMessage(uuid.NewV7(), c.ID, sender, body, photos, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AddMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	return m
}

func inbox(t *testing.T, s repository.Chats, viewer uuid.UUID, archived bool) []repository.InboxRow {
	t.Helper()
	rows, err := s.Inbox(context.Background(), viewer, archived, page.Request{})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestOpenPairOnce(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	ctx := context.Background()
	c, a, b := pair(t, db, s)
	again, err := s.OpenPair(ctx, domain.NewConversation(uuid.NewV7(), b, a, time.Now()))
	if err != nil || again.ID != c.ID {
		t.Fatalf("%v %v", again.ID, err)
	}
	for _, who := range []uuid.UUID{a, b} {
		if m, memberErr := s.Member(ctx, c.ID, who); memberErr != nil || m.AccountID != who {
			t.Fatalf("member: %+v %v", m, memberErr)
		}
	}
	if _, err = s.Get(ctx, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestMessagesAndInbox(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	ctx := context.Background()
	c, a, b := pair(t, db, s)
	first := send(t, s, c, a, "Còn không?")
	if err := s.AddMessage(ctx, first); err != nil {
		t.Fatal(err)
	}
	photo := send(t, s, c, b, "", uuid.NewV7())
	for _, c2 := range []struct {
		viewer uuid.UUID
		n      int
	}{{a, 1}, {b, 2}} {
		ms, err := s.Messages(ctx, c.ID, c2.viewer, page.Request{})
		if err != nil || len(ms) != c2.n {
			t.Fatalf("messages of %s: %d %v", c2.viewer, len(ms), err)
		}
	}
	rows := inbox(t, s, a, false)
	if len(rows) != 1 || rows[0].Last.ID() != first.ID() || rows[0].Other != b || rows[0].Unread != 0 {
		t.Fatalf("a's inbox: %+v", rows)
	}
	photo.Published(map[uuid.UUID]domain.Photo{photo.UploadIDs()[0]: {PhotoKey: "media/p.jpg", ThumbKey: "media/p_thumb.jpg"}})
	if err := s.SaveMessage(ctx, photo); err != nil {
		t.Fatal(err)
	}
	rows = inbox(t, s, a, false)
	if rows[0].Last.ID() != photo.ID() || rows[0].Unread != 1 {
		t.Fatalf("after the photo: %+v", rows)
	}
	if n, err := s.Unread(ctx, a); err != nil || n != 1 {
		t.Fatalf("unread %d %v", n, err)
	}
	if _, err := s.MarkRead(ctx, c.ID, a, photo.ID(), b); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Unread(ctx, a); n != 0 {
		t.Fatalf("unread after reading: %d", n)
	}
	if err := s.SaveMessage(ctx, photo); err != nil {
		t.Fatal(err)
	}
	var sent int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM events WHERE kind = 'message.sent' AND id = ANY(@ids)`,
		pgx.StrictNamedArgs{"ids": []uuid.UUID{first.ID(), photo.ID()}}).Scan(&sent); err != nil || sent != 2 {
		t.Fatalf("one message.sent per sent message: %d %v", sent, err)
	}
	got, err := s.GetMessage(ctx, photo.ID())
	if err != nil || got.Status() != domain.StatusSent || got.Photos()[0].PhotoKey != "media/p.jpg" {
		t.Fatalf("%+v %v", got.State(), err)
	}
	if err = s.DeleteMessage(ctx, first.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetMessage(ctx, first.ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}

func TestArchiveAndBlocks(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	ctx := context.Background()
	c, a, b := pair(t, db, s)
	send(t, s, c, b, "Chào")
	if err := s.SetArchived(ctx, c.ID, a, new(time.Now())); err != nil {
		t.Fatal(err)
	}
	if len(inbox(t, s, a, false)) != 0 || len(inbox(t, s, a, true)) != 1 {
		t.Fatal("archive")
	}
	if n, _ := s.Unread(ctx, a); n != 0 {
		t.Fatalf("archived unread counted: %d", n)
	}
	time.Sleep(10 * time.Millisecond)
	send(t, s, c, b, "Bạn ơi")
	if len(inbox(t, s, a, false)) != 1 || len(inbox(t, s, a, true)) != 0 {
		t.Fatal("a new message did not bring the conversation back")
	}
	if _, err := db.Exec(ctx, `INSERT INTO account_blocks (id, blocker_id, blocked_id, created_at) VALUES (@id, @a, @b, now())`,
		pgx.StrictNamedArgs{"id": uuid.NewV7(), "a": a, "b": b}); err != nil {
		t.Fatal(err)
	}
	if len(inbox(t, s, a, false)) != 0 {
		t.Fatal("the blocker still lists the conversation")
	}
	if len(inbox(t, s, b, false)) != 1 {
		t.Fatal("the blocked person lost it")
	}
}

func TestMarkReadNeverGoesBack(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	ctx := context.Background()
	c, a, b := pair(t, db, s)
	older, newer := send(t, s, c, b, "Một"), send(t, s, c, b, "Hai")
	if moved, err := s.MarkRead(ctx, c.ID, a, newer.ID(), b); err != nil || !moved {
		t.Fatalf("newer: %v %v", moved, err)
	}
	if moved, err := s.MarkRead(ctx, c.ID, a, older.ID(), b); err != nil || moved {
		t.Fatalf("older: %v %v", moved, err)
	}
	if m, err := s.Member(ctx, c.ID, a); err != nil || m.LastRead != newer.ID() {
		t.Fatalf("marker %v %v", m.LastRead, err)
	}
}

func TestReadKeepsTheArchive(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	ctx := context.Background()
	c, a, b := pair(t, db, s)
	m := send(t, s, c, b, "Chào")
	if err := s.SetArchived(ctx, c.ID, a, new(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkRead(ctx, c.ID, a, m.ID(), b); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Member(ctx, c.ID, a); err != nil || got.ArchivedAt == nil || got.LastRead != m.ID() {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.SetArchived(ctx, c.ID, a, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Member(ctx, c.ID, a); got.ArchivedAt != nil || got.LastRead != m.ID() {
		t.Fatalf("unarchive: %+v", got)
	}
}

func TestFindPair(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewChats(db)
	ctx := context.Background()
	c, a, b := pair(t, db, s)
	for _, p := range [][2]uuid.UUID{{a, b}, {b, a}} {
		if got, err := s.FindPair(ctx, p[0], p[1]); err != nil || got.ID != c.ID {
			t.Fatalf("%v: %v %v", p, got.ID, err)
		}
	}
	if _, err := s.FindPair(ctx, a, newAccount(t, db)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("no conversation: %v", err)
	}
}

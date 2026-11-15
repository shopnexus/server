package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/chat/repository"
	"github.com/shopnexus/server2/internal/chat/service"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// line is one sent message of a thread: who wrote it, how long before now,
// and the listing it names.
type line struct {
	from    uuid.UUID
	ago     time.Duration
	listing uuid.UUID
}

// thread writes a pair conversation of a and b with lines, oldest first,
// and returns its ID.
func thread(t *testing.T, db *pgxpool.Pool, now time.Time, a, b uuid.UUID, lines ...line) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if b.String() < a.String() {
		a, b = b, a
	}
	conv := uuid.NewV7()
	last := lines[len(lines)-1]
	if _, err := db.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, kind, created_at, last_message_at)
VALUES (@id, @a, @b, 'pair', @created, @last)`, pgx.StrictNamedArgs{
		"id": conv, "a": a, "b": b, "created": now.Add(-lines[0].ago), "last": now.Add(-last.ago),
	}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []uuid.UUID{a, b} {
		if _, err := db.Exec(ctx, `INSERT INTO conversation_members (conversation_id, account_id) VALUES (@c, @m)`,
			pgx.StrictNamedArgs{"c": conv, "m": m}); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range lines {
		var listing *uuid.UUID
		if l.listing != (uuid.UUID{}) {
			listing = &l.listing
		}
		if _, err := db.Exec(ctx, `INSERT INTO messages (id, conversation_id, sender_id, body, photos, listing_id, status, created_at)
VALUES (@id, @c, @from, 'Còn không bạn?', '[]', @listing, 'sent', @at)`, pgx.StrictNamedArgs{
			"id": uuid.NewV7(), "c": conv, "from": l.from, "listing": listing, "at": now.Add(-l.ago),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return conv
}

// The reply rate counts the conversations others started about the
// seller's listings in the last 90 days, once answered or a day old, and
// is null below five.
func TestReplyRate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	now := time.Now().Truncate(time.Second)
	s := service.New(service.Deps{Chats: repository.NewChats(db), Now: func() time.Time { return now }})
	seller := accounttest.New(t, db)
	mine := catalogtest.Listing(t, db, seller)
	hour, day := time.Hour, 24*time.Hour
	for _, ago := range []time.Duration{2 * day, 5 * day, 30 * day, 89 * day} {
		buyer := accounttest.New(t, db)
		thread(t, db, now, seller, buyer, line{buyer, ago, mine}, line{seller, ago - 3*hour, uuid.UUID{}})
	}
	// Younger than a day and unanswered: left out until answered or a day old.
	young := accounttest.New(t, db)
	thread(t, db, now, seller, young, line{young, 2 * hour, mine})
	// Started by the seller as a buyer, about the other person's listing.
	other := accounttest.New(t, db)
	thread(t, db, now, seller, other, line{seller, 3 * day, catalogtest.Listing(t, db, other)}, line{other, 3*day - hour, uuid.UUID{}})
	// Older than 90 days.
	old := accounttest.New(t, db)
	thread(t, db, now, seller, old, line{old, 91 * day, mine}, line{seller, 91*day - hour, uuid.UUID{}}, line{old, day, uuid.UUID{}})
	// A first message that names no listing.
	stranger := accounttest.New(t, db)
	thread(t, db, now, seller, stranger, line{stranger, 4 * day, uuid.UUID{}})
	if got, err := s.ReplyRate(ctx, seller); err != nil || got != nil {
		t.Fatalf("four count: %+v %v", got, err)
	}
	// Answered 25 hours after it started: counts, and not in time.
	late := accounttest.New(t, db)
	thread(t, db, now, seller, late, line{late, 3 * day, mine}, line{seller, 3*day - 25*hour, uuid.UUID{}})
	got, err := s.ReplyRate(ctx, seller)
	if err != nil || got == nil || *got != (service.ReplyRate{Percent: 80, Count: 5}) {
		t.Fatalf("five count: %+v %v", got, err)
	}
	if got, err = s.ReplyRate(ctx, late); err != nil || got != nil {
		t.Fatalf("a buyer has no rate: %+v %v", got, err)
	}
}

// A buyer's first message in a conversation their deal opened names no
// listing, and the conversation counts by the deal; without a deal of the
// seller's as seller it does not.
func TestReplyRateCountsByTheDeal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	now := time.Now().Truncate(time.Second)
	s := service.New(service.Deps{Chats: repository.NewChats(db), Now: func() time.Time { return now }})
	seller := accounttest.New(t, db)
	day := 24 * time.Hour
	for range 5 {
		buyer := accounttest.New(t, db)
		conv := thread(t, db, now, seller, buyer, line{buyer, 2 * day, uuid.UUID{}}, line{seller, 2*day - time.Hour, uuid.UUID{}})
		dealIn(t, db, conv, buyer, seller)
	}
	// The seller bought here: the deal is not about the seller's listing.
	other := accounttest.New(t, db)
	dealIn(t, db, thread(t, db, now, seller, other, line{other, 2 * day, uuid.UUID{}}), seller, other)
	got, err := s.ReplyRate(ctx, seller)
	if err != nil || got == nil || *got != (service.ReplyRate{Percent: 100, Count: 5}) {
		t.Fatalf("%+v %v", got, err)
	}
}

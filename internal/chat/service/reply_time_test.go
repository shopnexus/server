package service_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server/internal/account/accounttest"
	"github.com/shopnexus/server/internal/catalog/catalogtest"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
	"github.com/shopnexus/server/internal/chat/service"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// The reply time is the median wait over the seller's answers of the last
// 30 days to people who wrote about their listings. A message within an
// hour of the seller's last one continues the talk and starts no wait.
func TestReplyTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	now := time.Now().Truncate(time.Second)
	chats := repository.NewChats(db)
	s := service.New(service.Deps{Chats: chats, Now: func() time.Time { return now }})
	seller := accounttest.New(t, db)
	mine := catalogtest.Listing(t, db, seller)
	minute, hour, day := time.Minute, time.Hour, 24*time.Hour
	// say adds a sent message the way a send does, so it writes its sample.
	say := func(conv, from uuid.UUID, ago time.Duration) {
		t.Helper()
		m := domain.RestoreMessage(domain.MessageState{
			ID: uuid.NewV7(), ConversationID: conv, SenderID: from, Body: "Còn ạ", Status: domain.StatusSent, CreatedAt: now.Add(-ago),
		})
		if _, err := chats.AddMessage(ctx, m, uuid.Nil()); err != nil {
			t.Fatal(err)
		}
	}
	want := func(name string, w domain.ReplyTime) {
		t.Helper()
		if got, err := s.ReplyTime(ctx, seller); err != nil || got != w {
			t.Fatalf("%s: %q %v, want %q", name, got, err, w)
		}
	}

	// Four buyers answered in 5 minutes; one also follows up within the
	// hour, which is the same talk, and comes back the next day.
	var talk uuid.UUID
	for i := range 4 {
		buyer := accounttest.New(t, db)
		conv := thread(t, db, now, seller, buyer, line{buyer, 3 * day, mine})
		say(conv, seller, 3*day-5*minute)
		if i == 0 {
			talk = conv
			say(conv, buyer, 3*day-20*minute)
			say(conv, seller, 3*day-50*minute)
			say(conv, buyer, 2*day)
		}
	}
	// The seller as a buyer: their answers there are no seller's.
	other := accounttest.New(t, db)
	bought := thread(t, db, now, seller, other, line{seller, 2 * day, catalogtest.Listing(t, db, other)}, line{other, 2*day - hour, uuid.UUID{}})
	say(bought, seller, 2*day-3*hour)
	// An answer older than 30 days.
	old := accounttest.New(t, db)
	say(thread(t, db, now, seller, old, line{old, 40 * day, mine}), seller, 40*day-minute)
	want("four answers", "")

	// The next day's question, answered 3 hours on, is the fifth answer.
	say(talk, seller, 2*day-3*hour)
	want("five answers", domain.ReplyMinutes)
	for range 4 {
		buyer := accounttest.New(t, db)
		say(thread(t, db, now, seller, buyer, line{buyer, day, mine}), seller, day-3*hour)
	}
	want("nine answers", domain.ReplyHours)
}

// A sent message trims anyone's samples past 30 days, so a silent seller's
// go too, and the account's deletion takes its own.
func TestReplySamplesLeave(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	now := time.Now().Truncate(time.Second)
	chats := repository.NewChats(db)
	s := service.New(service.Deps{Chats: chats, QuickReplies: repository.NewQuickReplies(db), Now: func() time.Time { return now }})
	answer := func(seller uuid.UUID, ago time.Duration) {
		t.Helper()
		buyer := accounttest.New(t, db)
		conv := thread(t, db, now, seller, buyer, line{buyer, ago + time.Minute, catalogtest.Listing(t, db, seller)})
		m := domain.RestoreMessage(domain.MessageState{
			ID: uuid.NewV7(), ConversationID: conv, SenderID: seller, Body: "Còn ạ", Status: domain.StatusSent, CreatedAt: now.Add(-ago),
		})
		if _, err := chats.AddMessage(ctx, m, uuid.Nil()); err != nil {
			t.Fatal(err)
		}
	}
	samples := func(account uuid.UUID) int {
		t.Helper()
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM reply_samples WHERE account_id = @account`,
			pgx.StrictNamedArgs{"account": account}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	quiet, busy := accounttest.New(t, db), accounttest.New(t, db)
	answer(quiet, 40*24*time.Hour)
	if n := samples(quiet); n != 1 {
		t.Fatalf("%d samples before the trim", n)
	}
	answer(busy, 0)
	if q, b := samples(quiet), samples(busy); q != 0 || b != 1 {
		t.Fatalf("after another's send: quiet %d, busy %d", q, b)
	}
	if err := s.DeleteAccount(ctx, busy); err != nil {
		t.Fatal(err)
	}
	if n := samples(busy); n != 0 {
		t.Fatalf("%d samples after the deletion", n)
	}
}

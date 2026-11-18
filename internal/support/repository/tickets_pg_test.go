package repository_test

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/accounttest"
	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/chat/chattest"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/internal/support/domain"
	"github.com/shopnexus/server2/internal/support/repository"
)

var at = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)

// chat makes a support chat of person with ShopNexus, as chat's
// OpenSupport does.
func chat(t *testing.T, db *pgxpool.Pool, person uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	for _, q := range []string{
		`INSERT INTO conversations (id, account_a, account_b, kind, created_at)
VALUES (@id, LEAST(@p::uuid, @s::uuid), GREATEST(@p::uuid, @s::uuid), 'support', now())`,
		`INSERT INTO conversation_members (conversation_id, account_id) VALUES (@id, @p), (@id, @s)`,
	} {
		if _, err := db.Exec(context.Background(), q, pgx.StrictNamedArgs{"id": id, "p": person, "s": accountdomain.ShopNexusID}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// message adds a sent message of sender's to conversation.
func message(t *testing.T, db *pgxpool.Pool, conversation, sender uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `INSERT INTO messages (id, conversation_id, sender_id, body, photos, status, created_at)
VALUES (@id, @c, @s, 'x', '[]', 'sent', now())`, pgx.StrictNamedArgs{"id": id, "c": conversation, "s": sender}); err != nil {
		t.Fatal(err)
	}
	return id
}

// shippedOrder makes a shipped order of buyer's.
func shippedOrder(t *testing.T, db *pgxpool.Pool, buyer uuid.UUID) uuid.UUID {
	t.Helper()
	seller := accounttest.New(t, db)
	deal, order := uuid.NewV7(), uuid.NewV7()
	a := pgx.NamedArgs{
		"deal": deal, "order": order, "listing": catalogtest.Listing(t, db, seller), "buyer": buyer, "seller": seller,
		"conversation": chattest.Pair(t, db, buyer, seller),
	}
	for _, q := range []string{
		`INSERT INTO deals (id, conversation_id, listing_id, buyer_id, seller_id, status, listed_price, write_id, created_at,
    updated_at)
VALUES (@deal, @conversation, @listing, @buyer, @seller, 'negotiating', 1500000, @deal, now(), now())`,
		`INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name, recipient_phone,
    recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier, tracking_code, shipment_status, requested_at, shipped_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    'manual', 'ghtk', 'S1.A1', 'in_transit', now(), now())`,
	} {
		if _, err := db.Exec(context.Background(), q, a); err != nil {
			t.Fatal(err)
		}
	}
	return order
}

// member inserts a console member.
func member(t *testing.T, db *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	id := uuid.NewV7()
	if _, err := db.Exec(context.Background(), `
INSERT INTO console_members (id, email, display_name, console_role, status, password_hash, must_change_password,
                           credentials_version, totp_last_step, created_at, updated_at, write_id)
VALUES (@id, @email, @name, 'moderator', 'active', 'x', false, 1, 0, @at, @at, @id)`,
		pgx.StrictNamedArgs{"id": id, "email": id.String() + "@console.test", "name": name, "at": at}); err != nil {
		t.Fatal(err)
	}
	return id
}

func ticket(t *testing.T, db *pgxpool.Pool, person uuid.UUID, kind domain.Kind, order *uuid.UUID, carrier string) repository.Ticket {
	t.Helper()
	var seller *uuid.UUID
	if order != nil {
		if err := db.QueryRow(context.Background(), `SELECT seller_id FROM orders WHERE id = @id`,
			pgx.StrictNamedArgs{"id": *order}).Scan(&seller); err != nil {
			t.Fatal(err)
		}
	}
	return repository.Ticket{
		ID: uuid.NewV7(), AccountID: person, ConversationID: chat(t, db, person), Kind: kind, OrderID: order,
		SellerID: seller, CarrierName: carrier, OpenedAt: at,
	}
}

func open(t *testing.T, s repository.Tickets, tk repository.Ticket) repository.Ticket {
	t.Helper()
	out, existed, err := s.Insert(context.Background(), tk, uuid.NewV7(), domain.CarrierKey(tk.CarrierName), at.Add(-24*time.Hour))
	if err != nil || existed {
		t.Fatalf("insert: %v %v", existed, err)
	}
	return out
}

func actor(id uuid.UUID) *audit.Actor { return &audit.Actor{Kind: audit.ActorMember, MemberID: id} }

func TestInsertKeepsTheRateAndTheKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	person := accounttest.New(t, db)
	since := at.Add(-24 * time.Hour)
	key := uuid.NewV7()
	first, existed, err := s.Insert(ctx, ticket(t, db, person, domain.KindOther, nil, ""), key, "", since)
	if err != nil || existed || first.Status != domain.StatusOpen || first.LastFrom != domain.FromPerson || len(first.Events) != 1 {
		t.Fatalf("first: %+v %v %v", first, existed, err)
	}
	again, existed, err := s.Insert(ctx, ticket(t, db, person, domain.KindOther, nil, ""), key, "", since)
	if err != nil || !existed || again.ID != first.ID {
		t.Fatalf("the same key: %v %v %v", again.ID, existed, err)
	}
	// A retried step inserts the same ticket again: it is the step's own, so
	// the flow goes on to send the first message.
	retried := repository.Ticket{ID: first.ID, AccountID: person, ConversationID: first.ConversationID, Kind: domain.KindOther, OpenedAt: at}
	if got, mine, retryErr := s.Insert(ctx, retried, key, "", since); retryErr != nil || mine || got.ID != first.ID {
		t.Fatalf("the retried step: %v %v %v", got.ID, mine, retryErr)
	}
	for range domain.DailyLimit - 1 {
		open(t, s, ticket(t, db, person, domain.KindOther, nil, ""))
	}
	if _, _, err = s.Insert(ctx, ticket(t, db, person, domain.KindOther, nil, ""), uuid.NewV7(), "", since); !errors.Is(err, domain.ErrTooMany) {
		t.Fatalf("the eleventh: %v", err)
	}
}

func TestOneTicketPerOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	person := accounttest.New(t, db)
	order := shippedOrder(t, db, person)
	first := open(t, s, ticket(t, db, person, domain.KindNotReceived, &order, ""))
	second, existed, err := s.Insert(ctx, ticket(t, db, person, domain.KindNotReceived, &order, ""), uuid.NewV7(), "", at.Add(-24*time.Hour))
	if err != nil || !existed || second.ID != first.ID {
		t.Fatalf("%v %v %v", second.ID, existed, err)
	}
	if got, err := s.ByOrder(ctx, person, order); err != nil || got.ID != first.ID {
		t.Fatalf("by order: %v %v", got.ID, err)
	}
	if _, err := s.ByOrder(ctx, accounttest.New(t, db), order); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another account's: %v", err)
	}
}

func TestAPersonsMessageReopens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	person := accounttest.New(t, db)
	tk := open(t, s, ticket(t, db, person, domain.KindOther, nil, ""))
	if _, err := s.Resolve(ctx, tk.ID, nil, at); err != nil {
		t.Fatal(err)
	}
	reopened, err := s.Messaged(ctx, tk.ConversationID, uuid.NewV7(), domain.FromShopNexus, at)
	if err != nil || reopened {
		t.Fatalf("ShopNexus's message: %v %v", reopened, err)
	}
	if got, _ := s.Get(ctx, tk.ID); got.Status != domain.StatusResolved || got.LastFrom != domain.FromShopNexus {
		t.Fatalf("%+v", got)
	}
	reopened, err = s.Messaged(ctx, tk.ConversationID, uuid.NewV7(), domain.FromPerson, at.Add(time.Minute))
	if err != nil || !reopened {
		t.Fatalf("the person's message: %v %v", reopened, err)
	}
	got, _ := s.Get(ctx, tk.ID)
	if got.Status != domain.StatusOpen || got.ResolvedAt != nil || got.Events[len(got.Events)-1].Kind != "reopened" {
		t.Fatalf("%+v", got)
	}
	if reopened, err = s.Messaged(ctx, uuid.NewV7(), uuid.NewV7(), domain.FromPerson, at); err != nil || reopened {
		t.Fatalf("a chat with no ticket: %v %v", reopened, err)
	}
}

func TestResolveWritesTheAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	person, lan := accounttest.New(t, db), member(t, db, "Lan")
	tk := open(t, s, ticket(t, db, person, domain.KindOther, nil, ""))
	got, err := s.Resolve(ctx, tk.ID, actor(lan), at)
	if err != nil || got.Status != domain.StatusResolved || got.ResolvedBy == nil || *got.ResolvedBy != lan {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = s.Resolve(ctx, tk.ID, actor(lan), at); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("twice: %v", err)
	}
	if _, err = s.Reopen(ctx, tk.ID, actor(lan), at); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, uuid.NewV7(), actor(lan), at); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no ticket: %v", err)
	}
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM console_actions WHERE target_kind = 'ticket' AND target_id = @id
AND action_kind IN ('ticket.resolve', 'ticket.reopen')`, pgx.StrictNamedArgs{"id": tk.ID}).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit rows %d, %v", n, err)
	}
}

func TestRepliesNameTheirMember(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	person, lan := accounttest.New(t, db), member(t, db, "Lan")
	tk := open(t, s, ticket(t, db, person, domain.KindOther, nil, ""))
	byLan, auto := message(t, db, tk.ConversationID, accountdomain.ShopNexusID), message(t, db, tk.ConversationID, accountdomain.ShopNexusID)
	if err := s.RecordReply(ctx, tk.ID, byLan, actor(lan), at); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordReply(ctx, tk.ID, auto, nil, at); err != nil {
		t.Fatal(err)
	}
	got, err := s.Members(ctx, []uuid.UUID{byLan, auto, uuid.NewV7()})
	if err != nil || got[byLan].Member != "Lan" || got[byLan].Automatic || !got[auto].Automatic || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestQueueAndCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	person := accounttest.New(t, db)
	older := open(t, s, ticket(t, db, person, domain.KindOther, nil, ""))
	newer := open(t, s, ticket(t, db, person, domain.KindCarrierRequest, nil, "Ninja Van"))
	if _, err := s.Messaged(ctx, older.ConversationID, uuid.NewV7(), domain.FromPerson, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	q, err := s.Queue(ctx, domain.StatusOpen, nil, page.Request{Limit: 1})
	if err != nil || len(q) != 2 || q[0].ID != older.ID {
		t.Fatalf("newest activity first: %v %v", ids(q), err)
	}
	rest, err := s.Queue(ctx, domain.StatusOpen, nil, page.Request{Cursor: older.ID, Limit: 1})
	if err != nil || len(rest) != 1 || rest[0].ID != newer.ID {
		t.Fatalf("after the cursor: %v %v", ids(rest), err)
	}
	kind := domain.KindCarrierRequest
	if only, _ := s.Queue(ctx, domain.StatusOpen, &kind, page.Request{}); len(only) != 1 || only[0].CarrierName != "Ninja Van" {
		t.Fatalf("by kind: %v", ids(only))
	}
	if n, _ := s.CountOpenOf(ctx, person); n != 2 {
		t.Fatalf("open of %d", n)
	}
	if n, countErr := s.CountByStatus(ctx, time.Time{}); countErr != nil || !maps.Equal(n, map[domain.Status]int{domain.StatusOpen: 2}) {
		t.Fatalf("by status %v %v", n, countErr)
	}
	if n, countErr := s.CountByStatus(ctx, at.Add(time.Hour)); countErr != nil || len(n) != 0 {
		t.Fatalf("opened since a later time %v %v", n, countErr)
	}
	mine, err := s.OfAccount(ctx, person)
	if err != nil || len(mine) != 2 {
		t.Fatalf("of account %v %v", ids(mine), err)
	}
}

func TestSameCarrierCountsThirtyDays(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	for _, name := range []string{"Ninja Van", "ninja  VAN", "Ahamove"} {
		open(t, s, ticket(t, db, accounttest.New(t, db), domain.KindCarrierRequest, nil, name))
	}
	if n, err := s.SameCarrier(ctx, domain.CarrierKey("NINJA VAN"), at.Add(-domain.SameCarrierWindow)); err != nil || n != 2 {
		t.Fatalf("got %d %v", n, err)
	}
}

func TestResolveAllOfAnAccount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	s := repository.NewTickets(db)
	person := accounttest.New(t, db)
	open(t, s, ticket(t, db, person, domain.KindOther, nil, ""))
	open(t, s, ticket(t, db, person, domain.KindOther, nil, ""))
	if err := s.ResolveAllOf(ctx, person, at); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountOpenOf(ctx, person); n != 0 {
		t.Fatalf("still open %d", n)
	}
}

func ids(ts []repository.Ticket) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ts))
	for i := range ts {
		out = append(out, ts[i].ID)
	}
	return out
}

package migrations_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	supportdomain "github.com/shopnexus/server2/internal/support/domain"
	supportrepo "github.com/shopnexus/server2/internal/support/repository"
	"github.com/shopnexus/server2/migrations"
)

// shopNexus is the system account 0044 makes.
const shopNexus = "0193e000-0000-7000-8000-000000000001"

// Feedback sent before 0044 arrives as an open ticket of kind other, its
// text the first message of a support chat with ShopNexus.
func TestSupportTicketsMoveTheFeedback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0044_support_tickets.sql")); err != nil {
		t.Fatal(err)
	}
	person := accounttest.New(t, db)
	if _, err := db.Exec(ctx, `INSERT INTO feedback (id, account_id, body, request_key, created_at)
VALUES (uuidv7(), @person, 'Lọc khu vực hay nhảy về Q.1', uuidv7(), now() - interval '2 days')`,
		pgx.StrictNamedArgs{"person": person}); err != nil {
		t.Fatal(err)
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatalf("0044 on a database with feedback: %v", err)
	}
	var kind, status, body, convKind string
	var fromPerson bool
	var events int
	if err := db.QueryRow(ctx, `SELECT t.kind, t.status, m.body, m.sender_id = t.account_id, c.kind,
       (SELECT count(*) FROM ticket_events e WHERE e.ticket_id = t.id AND e.kind = 'opened')
FROM tickets t JOIN messages m ON m.id = t.first_message_id JOIN conversations c ON c.id = t.conversation_id
WHERE t.account_id = @person`, pgx.StrictNamedArgs{"person": person}).
		Scan(&kind, &status, &body, &fromPerson, &convKind, &events); err != nil {
		t.Fatal(err)
	}
	if kind != "other" || status != "open" || body != "Lọc khu vực hay nhảy về Q.1" || !fromPerson || convKind != "support" || events != 1 {
		t.Fatalf("got %s %s %q from person %v, conversation %s, %d opened events", kind, status, body, fromPerson, convKind, events)
	}

	var feedback *string
	if err := db.QueryRow(ctx, `SELECT to_regclass('feedback')::text`).Scan(&feedback); err != nil || feedback != nil {
		t.Fatalf("feedback is still there: %v %v", feedback, err)
	}
}

// A pair of people has one chat; a person and ShopNexus have one per
// ticket.
func TestSupportChatsAreManyPerPair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	person, other := accounttest.New(t, db), accounttest.New(t, db)
	insert := func(with uuid.UUID, kind string) error {
		_, err := db.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, kind, created_at)
VALUES (uuidv7(), LEAST(@one::uuid, @other::uuid), GREATEST(@one::uuid, @other::uuid), @kind, now())`,
			pgx.StrictNamedArgs{"one": person, "other": with, "kind": kind})
		return err
	}
	system := uuid.MustParse(shopNexus)
	for name, err := range map[string]error{
		"a first support chat":  insert(system, "support"),
		"a second support chat": insert(system, "support"),
		"a pair chat":           insert(other, "pair"),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := insert(other, "pair"); err == nil {
		t.Error("a second pair chat was stored")
	}
}

// An order completed before 0044 takes completed_by from its deal's
// completed move.
func TestOrdersCompletedByComesFromTheMove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0044_support_tickets.sql")); err != nil {
		t.Fatal(err)
	}
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	orders := map[string]uuid.UUID{}
	for _, by := range []string{"system", "buyer"} {
		deal, order := uuid.NewV7(), uuid.NewV7()
		orders[by] = order
		a := pgx.NamedArgs{"deal": deal, "order": order, "listing": catalogtest.Listing(t, db, seller), "buyer": buyer, "seller": seller, "by": by, "at": time.Now()}
		for _, q := range []string{
			insertDeal, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name,
    recipient_phone, recipient_street, recipient_commune, shipment_mode, carrier, tracking_code, shipment_status, requested_at,
    shipped_at, closed_at, delivered_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'completed', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004',
    'manual', 'ghtk', 'S1.A1', 'delivered', @at, @at, @at, @at)`,
			`INSERT INTO deal_moves (id, deal_id, kind, by_party, moved_at) VALUES (uuidv7(), @deal, 'completed', @by, now())`,
		} {
			if _, err := db.Exec(ctx, q, a); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for by, order := range orders {
		var got string
		if err := db.QueryRow(ctx, `SELECT completed_by FROM orders WHERE id = @id`, pgx.StrictNamedArgs{"id": order}).Scan(&got); err != nil || got != by {
			t.Errorf("%s: completed_by %q, %v", by, got, err)
		}
	}
}

// A ticket carries what its kind needs, and an order has one report.
func TestTicketChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	person, seller := accounttest.New(t, db), accounttest.New(t, db)
	deal, order := uuid.NewV7(), uuid.NewV7()
	a := pgx.NamedArgs{"deal": deal, "order": order, "listing": catalogtest.Listing(t, db, seller), "buyer": person, "seller": seller}
	for _, q := range []string{insertPairDeal, `INSERT INTO orders (id, deal_id, listing_id, buyer_id, seller_id, status, price, recipient_name,
    recipient_phone, recipient_street, recipient_commune, recipient_kind, shipment_mode, carrier, tracking_code, shipment_status, requested_at, shipped_at)
VALUES (@order, @deal, @listing, @buyer, @seller, 'shipped', 1500000, 'Hân', '+84912345678', '1 Lê Lợi', '00004', 'home',
    'manual', 'ghtk', 'S1.A1', 'in_transit', now(), now())`} {
		if _, err := db.Exec(ctx, q, a); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	insert := func(kind string, order *uuid.UUID, carrier string) error {
		conv := uuid.NewV7()
		if _, err := db.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, kind, created_at)
VALUES (@id, LEAST(@p::uuid, @s::uuid), GREATEST(@p::uuid, @s::uuid), 'support', now())`,
			pgx.StrictNamedArgs{"id": conv, "p": person, "s": uuid.MustParse(shopNexus)}); err != nil {
			t.Fatal(err)
		}
		var key any
		if carrier != "" {
			key = carrier
		}
		var sellerOf any
		if order != nil {
			sellerOf = seller
		}
		_, err := db.Exec(ctx, `INSERT INTO tickets (id, account_id, conversation_id, kind, status, order_id, seller_id, carrier_name,
    carrier_key, last_message_at, last_from, opened_at, request_key)
VALUES (uuidv7(), @person, @conv, @kind, 'open', @order, @seller, @carrier, @key, now(), 'person', now(), uuidv7())`,
			pgx.StrictNamedArgs{
				"person": person, "conv": conv, "kind": kind, "order": order, "seller": sellerOf, "carrier": null(carrier), "key": key,
			})
		return err
	}
	for name, err := range map[string]error{
		"a report":          insert("not_received", &order, ""),
		"a carrier request": insert("carrier_request", nil, "Ninja Van"),
		"other":             insert("other", nil, ""),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, err := range map[string]error{
		"a second report of the order": insert("not_received", &order, ""),
		"a report without an order":    insert("not_received", nil, ""),
		"a carrier request unnamed":    insert("carrier_request", nil, ""),
		"other with an order":          insert("other", &order, ""),
	} {
		if err == nil {
			t.Errorf("%s was stored", name)
		}
	}
}

func TestTicketHistoryReplayAfter0048KeepsLaterResolution(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0048_order_screens.sql")); err != nil {
		t.Fatal(err)
	}
	owner := accounttest.New(t, db)
	conversation := uuid.NewV7()
	if _, err := db.Exec(ctx, `INSERT INTO conversations (id,account_a,account_b,kind,created_at)
VALUES (@id,least(@owner::uuid,@system::uuid),greatest(@owner::uuid,@system::uuid),'support',now())`, pgx.StrictNamedArgs{"id": conversation, "owner": owner, "system": uuid.MustParse(shopNexus)}); err != nil {
		t.Fatal(err)
	}
	openedAt := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	// The ticket is written in SQL: the repository is today's and reads
	// columns later migrations add.
	repo := supportrepo.NewTickets(db)
	tk := supportrepo.Ticket{ID: uuid.NewV7()}
	_, err := db.Exec(ctx, `INSERT INTO tickets (id, account_id, conversation_id, kind, status, last_message_at, last_from, opened_at,
    request_key)
VALUES (@id, @owner, @conversation, 'other', 'open', @at, 'person', @at, uuidv7())`,
		pgx.StrictNamedArgs{"id": tk.ID, "owner": owner, "conversation": conversation, "at": openedAt})
	if err != nil {
		t.Fatal(err)
	}
	ticketEvent := `INSERT INTO ticket_events (id, ticket_id, kind, at) VALUES (uuidv7(), @id, @kind, @at)`
	if _, err = db.Exec(ctx, ticketEvent, pgx.StrictNamedArgs{"id": tk.ID, "kind": "opened", "at": openedAt}); err != nil {
		t.Fatal(err)
	}
	initial, oldReply, newReply := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	for i, id := range []uuid.UUID{initial, oldReply, newReply} {
		sentAt := openedAt.Add(time.Duration(i*2) * time.Minute)
		if _, err = db.Exec(ctx, `INSERT INTO messages (id,conversation_id,sender_id,body,photos,status,created_at)
VALUES (@id,@conversation,@owner,'reply','[]','sent',@at)`, pgx.StrictNamedArgs{"id": id, "conversation": conversation, "owner": owner, "at": sentAt}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(ctx, `UPDATE tickets SET first_message_id = @initial, last_message_at = @at, last_from = 'person' WHERE id = @id`,
		pgx.StrictNamedArgs{"initial": initial, "at": openedAt.Add(2 * time.Minute), "id": tk.ID}); err != nil {
		t.Fatal(err)
	}
	resolvedAt := openedAt.Add(3 * time.Minute)
	if _, err = db.Exec(ctx, `UPDATE tickets SET status = 'resolved', resolved_at = @at WHERE id = @id`,
		pgx.StrictNamedArgs{"id": tk.ID, "at": resolvedAt}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, ticketEvent, pgx.StrictNamedArgs{"id": tk.ID, "kind": "resolved", "at": resolvedAt}); err != nil {
		t.Fatal(err)
	}
	if _, err = postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var receipts int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM ticket_message_receipts`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("migration consumed historical events: %d %v", receipts, err)
	}
	if reopened, replayErr := repo.Messaged(ctx, conversation, oldReply, supportdomain.FromPerson, openedAt.Add(2*time.Minute)); replayErr != nil || reopened {
		t.Errorf("legacy processed reply reopened: %v %v", reopened, replayErr)
	}
	got, err := repo.Get(ctx, tk.ID)
	if err != nil || got.Status != supportdomain.StatusResolved {
		t.Errorf("legacy resolution lost: %+v %v", got, err)
	}
	if reopened, deliveryErr := repo.Messaged(ctx, conversation, newReply, supportdomain.FromPerson, openedAt.Add(4*time.Minute)); deliveryErr != nil || !reopened {
		t.Errorf("legitimate unprocessed newer reply: %v %v", reopened, deliveryErr)
	}
}

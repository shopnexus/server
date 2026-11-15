package migrations_test

import (
	"context"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/shopnexus/server2/internal/account/accounttest"
	"github.com/shopnexus/server2/internal/catalog/catalogtest"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
	"github.com/shopnexus/server2/migrations"
)

// The deals of the release before take their pair's conversation, both
// ways: a pair that had one keeps it, and a pair without one gets one with
// both members.
func TestDealsTakeThePairsConversation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.Empty(t)
	if _, err := postgres.Migrate(ctx, db, before(t, "0047_deal_conversation.sql")); err != nil {
		t.Fatal(err)
	}
	seller, talked, silent := accounttest.New(t, db), accounttest.New(t, db), accounttest.New(t, db)
	existing := uuid.NewV7()
	if _, err := db.Exec(ctx, `INSERT INTO conversations (id, account_a, account_b, kind, created_at)
VALUES (@id, LEAST(@a::uuid, @b::uuid), GREATEST(@a::uuid, @b::uuid), 'pair', now())`,
		pgx.StrictNamedArgs{"id": existing, "a": seller, "b": talked}); err != nil {
		t.Fatal(err)
	}
	// talked buys twice from seller and sells once to seller; silent buys
	// once and never wrote.
	deals := map[uuid.UUID][2]uuid.UUID{} // deal → buyer, seller
	for _, p := range [][2]uuid.UUID{{talked, seller}, {talked, seller}, {seller, talked}, {silent, seller}} {
		id := uuid.NewV7()
		deals[id] = p
		if _, err := db.Exec(ctx, insertDeal, pgx.NamedArgs{
			"deal": id, "listing": catalogtest.Listing(t, db, p[1]), "buyer": p[0], "seller": p[1],
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := postgres.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	var opened uuid.UUID
	for id, p := range deals {
		var got uuid.UUID
		if err := db.QueryRow(ctx, `SELECT conversation_id FROM deals WHERE id = @id`, pgx.StrictNamedArgs{"id": id}).
			Scan(&got); err != nil {
			t.Fatal(err)
		}
		switch {
		case p[0] == silent:
			opened = got
		case got != existing:
			t.Errorf("deal %v of the pair that talked: %s, want %s", p, got, existing)
		default:
		}
	}
	if opened == existing || opened == (uuid.UUID{}) {
		t.Fatalf("the silent pair's conversation: %s", opened)
	}
	var members int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM conversation_members WHERE conversation_id = @id
AND account_id IN (@a, @b)`, pgx.StrictNamedArgs{"id": opened, "a": seller, "b": silent}).Scan(&members); err != nil || members != 2 {
		t.Fatalf("members of the opened conversation: %d %v", members, err)
	}
	var pairs int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE kind = 'pair'`).Scan(&pairs); err != nil || pairs != 2 {
		t.Fatalf("pair conversations: %d %v", pairs, err)
	}
	if _, err := db.Exec(ctx, insertDeal, pgx.NamedArgs{
		"deal": uuid.NewV7(), "listing": catalogtest.Listing(t, db, seller), "buyer": silent, "seller": seller,
	}); err == nil {
		t.Error("a deal went in without a conversation")
	}
}

// A deal message names a deal and a move and nothing else; a message
// without a deal has no move.
func TestDealMessageChecks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	seller, buyer := accounttest.New(t, db), accounttest.New(t, db)
	deal := uuid.NewV7()
	if _, err := db.Exec(ctx, insertPairDeal, pgx.NamedArgs{
		"deal": deal, "listing": catalogtest.Listing(t, db, seller), "buyer": buyer, "seller": seller,
	}); err != nil {
		t.Fatal(err)
	}
	insert := func(body, status string, dealID any, kind, by any) error {
		_, err := db.Exec(ctx, `INSERT INTO messages (id, conversation_id, sender_id, body, photos, status, created_at, deal_id,
    move_kind, move_by)
SELECT uuidv7(), conversation_id, @buyer, @body, '[]', @status, now(), @deal, @kind, @by FROM deals WHERE id = @id`,
			pgx.StrictNamedArgs{"buyer": buyer, "body": body, "status": status, "deal": dealID, "kind": kind, "by": by, "id": deal})
		return err
	}
	if err := insert("", "sent", deal, "offer", "buyer"); err != nil {
		t.Fatalf("a deal message: %v", err)
	}
	if err := insert("Còn không?", "sent", nil, nil, nil); err != nil {
		t.Fatalf("a text message: %v", err)
	}
	for name, err := range map[string]error{
		"a deal message with text":      insert("Bớt nhé", "sent", deal, "offer", "buyer"),
		"a deal message not sent":       insert("", "processing", deal, "offer", "buyer"),
		"a deal message without a side": insert("", "sent", deal, "offer", nil),
		"a move without a deal":         insert("", "sent", nil, "offer", "buyer"),
		"a side that is no side":        insert("", "sent", deal, "offer", "admin"),
	} {
		if err == nil {
			t.Errorf("%s went in", name)
		}
	}
}

package repository_test

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// markDeleted turns id into a tombstone, as Erase leaves it.
func markDeleted(t *testing.T, db *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE accounts
SET status = 'deleted', deleted_at = now(), display_name = NULL, bio = NULL, avatar_key = NULL WHERE id = @id`,
		pgx.StrictNamedArgs{"id": id}); err != nil {
		t.Fatal(err)
	}
}

// A write that passed its checks before the account was deleted, and
// commits after, writes nothing on the tombstone.
func TestDeletedAccountKeepsNoNewRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts, book := repository.NewAccounts(db), repository.NewAddresses(db)
	gone, other := seed(t, accounts), seed(t, accounts)
	markDeleted(t, db, gone)

	a, err := domain.NewAddress(uuid.NewV7(), gone, domain.AddressDraft{Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: domain.AddressHome}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = book.Insert(ctx, uuid.NewV7(), &a, false); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("an address of a deleted account: %v", err)
	}
	if err = accounts.Block(ctx, uuid.NewV7(), gone, other, now); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("a block by a deleted account: %v", err)
	}
	if n := count(t, db, `SELECT (SELECT count(*) FROM addresses WHERE account_id = @id) + (SELECT count(*) FROM account_blocks WHERE blocker_id = @id)`, gone); n != 0 {
		t.Fatalf("rows on the tombstone: %d", n)
	}
}

// An edit that makes an address the default, read before the account was
// deleted and saved after, is refused as Insert refuses one, and the
// default stays.
func TestDeletedAccountTakesNoDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	accounts, book := repository.NewAccounts(db), repository.NewAddresses(db)
	gone := seed(t, accounts)
	as := insert(t, book, gone, 2)
	markDeleted(t, db, gone)
	edit := as[1]
	if err := book.Save(ctx, &edit, uuid.NewV7(), true); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("an edit as the default of a deleted account's address: %v", err)
	}
	if d := defaults(t, book, gone); len(d) != 1 || d[0] != as[0].ID() {
		t.Fatalf("defaults %v", d)
	}
}

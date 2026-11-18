package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/account/repository"
	"github.com/shopnexus/server/internal/platform/postgres/pgtest"
)

// address builds an address of owner's in commune 00004.
func address(t *testing.T, owner uuid.UUID) domain.Address {
	t.Helper()
	a, err := domain.NewAddress(uuid.NewV7(), owner, domain.AddressDraft{
		Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004", Kind: domain.AddressHome,
	}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// newBook is the accounts and the address books of one fresh database.
func newBook(t *testing.T) (repository.Accounts, repository.Addresses) {
	t.Helper()
	db := pgtest.New(t)
	return repository.NewAccounts(db), repository.NewAddresses(db)
}

// insert stores n addresses of owner's and returns them, oldest first.
func insert(t *testing.T, book repository.Addresses, owner uuid.UUID, n int) []domain.Address {
	t.Helper()
	out := make([]domain.Address, 0, n)
	for range n {
		a := address(t, owner)
		if err := book.Insert(context.Background(), uuid.NewV7(), &a, false); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// defaults lists the IDs of owner's default addresses.
func defaults(t *testing.T, book repository.Addresses, owner uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := book.List(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	var out []uuid.UUID
	for i := range rows {
		if rows[i].Address.IsDefault() {
			out = append(out, rows[i].Address.ID())
		}
	}
	return out
}

func TestAddresses(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	as := insert(t, book, me, 3)
	if !as[0].IsDefault() || as[1].IsDefault() {
		t.Fatalf("the first address is the default: %v %v", as[0].IsDefault(), as[1].IsDefault())
	}
	rows, err := book.List(ctx, me)
	if err != nil || len(rows) != 3 || rows[0].Address.ID() != as[0].ID() || rows[1].Address.ID() != as[2].ID() || rows[2].Address.ID() != as[1].ID() {
		t.Fatalf("the default first, then the newest: %+v %v", rows, err)
	}
	first := rows[0]
	if first.Address.Name() != "Hân" || first.Address.Phone() != "+84912345678" || first.Address.Kind() != domain.AddressHome || first.Address.AccountID() != me ||
		first.Address.WriteID() != as[0].WriteID() || first.Place.Commune == "" || first.Place.Province == "" || first.Address.CreatedAt().IsZero() {
		t.Fatalf("%+v", first)
	}
	got, err := book.Get(ctx, me, as[1].ID())
	if err != nil || got.Name() != "Hân" || got.WriteID() != as[1].WriteID() || got.IsDefault() {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = book.Get(ctx, other, as[1].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if rows, _ = book.List(ctx, other); len(rows) != 0 {
		t.Fatalf("someone else's list: %+v", rows)
	}
	place, err := book.Commune(ctx, "00004")
	if err != nil || place != first.Place {
		t.Fatalf("%+v %v", place, err)
	}
	if _, err = book.Commune(ctx, "nope"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("an unknown commune: %v", err)
	}
}

func TestSaveAddressWriteID(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me := seed(t, s)
	a := insert(t, book, me, 1)[0]
	stale := a
	if err := a.Edit(domain.AddressDraft{Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004", Kind: domain.AddressWork}, true); err != nil {
		t.Fatal(err)
	}
	n := uuid.NewV7()
	if err := book.Save(ctx, &a, n, false); err != nil {
		t.Fatal(err)
	}
	retry := stale
	if err := retry.Edit(domain.AddressDraft{Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004", Kind: domain.AddressWork}, true); err != nil {
		t.Fatal(err)
	}
	if err := book.Save(ctx, &retry, n, false); err != nil {
		t.Fatalf("a retry with the same write ID: %v", err)
	}
	if err := book.Save(ctx, &stale, uuid.NewV7(), false); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a stale save: %v", err)
	}
	if err := book.Save(ctx, &a, uuid.NewV7(), false); err != nil {
		t.Fatalf("a second save in a row: %v", err)
	}
	got, err := book.Get(ctx, me, a.ID())
	if err != nil || got.Name() != "Minh" || got.Phone() != "+84987654321" || got.Kind() != domain.AddressWork || got.WriteID() != a.WriteID() || !got.IsDefault() {
		t.Fatalf("%+v %v", got, err)
	}
	// Moving the default is a write too: a save read before it conflicts.
	other := insert(t, book, me, 1)[0]
	if err = book.SetDefault(ctx, me, other.ID()); err != nil {
		t.Fatal(err)
	}
	if err = book.Save(ctx, &a, uuid.NewV7(), false); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a save read before the default moved: %v", err)
	}
}

func TestAddressLimit(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me := seed(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, domain.MaxAddresses+5)
	for range domain.MaxAddresses + 5 {
		wg.Go(func() {
			a := address(t, me)
			errs <- book.Insert(ctx, uuid.NewV7(), &a, false)
		})
	}
	wg.Wait()
	close(errs)
	refused := 0
	for err := range errs {
		switch {
		case errors.Is(err, domain.ErrAddressLimit):
			refused++
		case err != nil:
			t.Fatal(err)
		default:
		}
	}
	rows, err := book.List(ctx, me)
	if err != nil || len(rows) != domain.MaxAddresses || refused != 5 {
		t.Fatalf("%d stored, %d refused: %v", len(rows), refused, err)
	}
	if d := defaults(t, book, me); len(d) != 1 {
		t.Fatalf("defaults %v", d)
	}
}

func TestSetDefaultAddress(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	as := insert(t, book, me, 3)
	for range 2 {
		if err := book.SetDefault(ctx, me, as[1].ID()); err != nil {
			t.Fatal(err)
		}
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != as[1].ID() {
		t.Fatalf("defaults %v", d)
	}
	if err := book.SetDefault(ctx, other, as[2].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if err := book.SetDefault(ctx, me, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("an unknown one: %v", err)
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != as[1].ID() {
		t.Fatalf("a refused move changed the default: %v", d)
	}
}

func TestDeleteAddressMovesTheDefault(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	as := insert(t, book, me, 3)
	if err := book.Delete(ctx, other, as[0].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if err := book.Delete(ctx, me, as[1].ID()); err != nil {
		t.Fatal(err)
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != as[0].ID() {
		t.Fatalf("deleting another address moved the default: %v", d)
	}
	if err := book.Delete(ctx, me, as[0].ID()); err != nil {
		t.Fatal(err)
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != as[2].ID() {
		t.Fatalf("the newest remaining is the default: %v", d)
	}
	if err := book.Delete(ctx, me, as[0].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleting again: %v", err)
	}
	if _, err := book.Get(ctx, me, as[0].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("the row is gone: %v", err)
	}
	if err := book.Delete(ctx, me, as[2].ID()); err != nil {
		t.Fatal(err)
	}
	if fresh := insert(t, book, me, 1)[0]; !fresh.IsDefault() {
		t.Fatal("the first address after emptying the book is the default")
	}
}

// TestAddressRequestKey: a request key the account used already returns
// the address that request added, at the cap too, and adds nothing.
func TestAddressRequestKey(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	key := uuid.NewV7()
	first := address(t, me)
	if err := book.Insert(ctx, key, &first, false); err != nil {
		t.Fatal(err)
	}
	insert(t, book, me, domain.MaxAddresses-1)
	again := address(t, me)
	if err := book.Insert(ctx, key, &again, false); err != nil || again.ID() != first.ID() || !again.IsDefault() {
		t.Fatalf("retry: %v %v %v", err, again.ID(), again.IsDefault())
	}
	if rows, err := book.List(ctx, me); err != nil || len(rows) != domain.MaxAddresses {
		t.Fatalf("%d stored: %v", len(rows), err)
	}
	theirs := address(t, other)
	if err := book.Insert(ctx, key, &theirs, false); err != nil || theirs.ID() == first.ID() {
		t.Fatalf("another account's key: %v", err)
	}
}

// TestInsertAsDefault: an address added with makeDefault takes the default
// in the insert's transaction, and a retried key moves nothing.
func TestInsertAsDefault(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me := seed(t, s)
	first := insert(t, book, me, 1)[0]
	key := uuid.NewV7()
	second := address(t, me)
	if err := book.Insert(ctx, key, &second, true); err != nil || !second.IsDefault() {
		t.Fatalf("%v, default %v", err, second.IsDefault())
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != second.ID() {
		t.Fatalf("defaults %v", d)
	}
	if old, err := book.Get(ctx, me, first.ID()); err != nil || old.WriteID() == first.WriteID() {
		t.Fatalf("the old default keeps its write ID: %v", err)
	}
	if err := book.SetDefault(ctx, me, first.ID()); err != nil {
		t.Fatal(err)
	}
	again := address(t, me)
	if err := book.Insert(ctx, key, &again, true); err != nil || again.ID() != second.ID() || again.IsDefault() {
		t.Fatalf("retry: %v %v, default %v", err, again.ID(), again.IsDefault())
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != first.ID() {
		t.Fatalf("a retried key moved the default: %v", d)
	}
}

// TestSaveAsDefault: an edit with makeDefault takes the default in the
// edit's transaction. An edit without it leaves the default where it is,
// and a stale edit with it moves nothing.
func TestSaveAsDefault(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me := seed(t, s)
	as := insert(t, book, me, 2)
	b, stale := as[1], as[1]
	w := uuid.NewV7()
	if err := book.Save(ctx, &b, w, true); err != nil || !b.IsDefault() || b.WriteID() != w {
		t.Fatalf("%v, default %v", err, b.IsDefault())
	}
	if got, err := book.Get(ctx, me, b.ID()); err != nil || !got.IsDefault() || got.WriteID() != w {
		t.Fatalf("stored: %+v %v", got, err)
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != b.ID() {
		t.Fatalf("defaults %v", d)
	}
	if err := book.Save(ctx, &b, uuid.NewV7(), true); err != nil || !b.IsDefault() {
		t.Fatalf("the default saved as the default: %v", err)
	}
	if err := book.Save(ctx, &b, uuid.NewV7(), false); err != nil || !b.IsDefault() {
		t.Fatalf("the default saved without makeDefault: %v", err)
	}
	a, err := book.Get(ctx, me, as[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	if err = book.Save(ctx, &a, uuid.NewV7(), false); err != nil || a.IsDefault() {
		t.Fatalf("another saved without makeDefault: %v, default %v", err, a.IsDefault())
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != b.ID() {
		t.Fatalf("an edit without makeDefault moved the default: %v", d)
	}
	if err = book.Save(ctx, &stale, uuid.NewV7(), true); !errors.Is(err, repository.ErrConflict) || stale.IsDefault() {
		t.Fatalf("a stale save: %v", err)
	}
	if d := defaults(t, book, me); len(d) != 1 || d[0] != b.ID() {
		t.Fatalf("a stale save moved the default: %v", d)
	}
}

// Inserts and edits that each make their address the default, all at once,
// leave one default: the advisory lock orders them. An edit of an address
// that lost the default after the edit read it conflicts, as after
// SetDefault.
func TestAddressDefaultRace(t *testing.T) {
	t.Parallel()
	s, book := newBook(t)
	ctx := context.Background()
	me := seed(t, s)
	as := insert(t, book, me, 5)
	var wg sync.WaitGroup
	saves, inserts := make(chan error, len(as)), make(chan error, 3)
	for i := range as {
		wg.Go(func() {
			a := as[i]
			saves <- book.Save(ctx, &a, uuid.NewV7(), true)
		})
	}
	for range 3 {
		wg.Go(func() {
			a := address(t, me)
			inserts <- book.Insert(ctx, uuid.NewV7(), &a, true)
		})
	}
	wg.Wait()
	close(saves)
	close(inserts)
	for err := range saves {
		if err != nil && !errors.Is(err, repository.ErrConflict) {
			t.Fatal(err)
		}
	}
	for err := range inserts {
		if err != nil {
			t.Fatal(err)
		}
	}
	if d := defaults(t, book, me); len(d) != 1 {
		t.Fatalf("defaults %v", d)
	}
}

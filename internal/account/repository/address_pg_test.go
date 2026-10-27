package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/account/repository"
)

// address builds an address of owner's in commune 00004.
func address(t *testing.T, owner uuid.UUID) domain.Address {
	t.Helper()
	a, err := domain.NewAddress(uuid.NewV7(), owner, domain.AddressDraft{
		Name: "Hân", Phone: "0912345678", Street: "12 Lý Thường Kiệt", CommuneCode: "00004",
	}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// insert stores n addresses of owner's and returns them, oldest first.
func insert(t *testing.T, s repository.Accounts, owner uuid.UUID, n int) []domain.Address {
	t.Helper()
	out := make([]domain.Address, 0, n)
	for range n {
		a := address(t, owner)
		if err := s.InsertAddress(context.Background(), uuid.NewV7(), &a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

// defaults lists the IDs of owner's default addresses.
func defaults(t *testing.T, s repository.Accounts, owner uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := s.ListAddresses(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	var out []uuid.UUID
	for i := range rows {
		if rows[i].IsDefault {
			out = append(out, rows[i].ID)
		}
	}
	return out
}

func TestAddresses(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	as := insert(t, s, me, 3)
	if !as[0].IsDefault() || as[1].IsDefault() {
		t.Fatalf("the first address is the default: %v %v", as[0].IsDefault(), as[1].IsDefault())
	}
	rows, err := s.ListAddresses(ctx, me)
	if err != nil || len(rows) != 3 || rows[0].ID != as[0].ID() || rows[1].ID != as[2].ID() || rows[2].ID != as[1].ID() {
		t.Fatalf("the default first, then the newest: %+v %v", rows, err)
	}
	first := rows[0]
	if first.Name != "Hân" || first.Phone != "+84912345678" || first.Commune == "" || first.Province == "" || first.CreatedAt.IsZero() {
		t.Fatalf("%+v", first)
	}
	got, err := s.GetAddress(ctx, me, as[1].ID())
	if err != nil || got.Name() != "Hân" || got.WriteID() != as[1].WriteID() || got.IsDefault() {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = s.GetAddress(ctx, other, as[1].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if rows, _ = s.ListAddresses(ctx, other); len(rows) != 0 {
		t.Fatalf("someone else's list: %+v", rows)
	}
	place, err := s.Commune(ctx, "00004")
	if err != nil || place.Commune != first.Commune || place.Province != first.Province {
		t.Fatalf("%+v %v", place, err)
	}
	if _, err = s.Commune(ctx, "nope"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("an unknown commune: %v", err)
	}
}

func TestSaveAddressWriteID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me := seed(t, s)
	a := insert(t, s, me, 1)[0]
	stale := a
	if err := a.Edit(domain.AddressDraft{Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004"}, true); err != nil {
		t.Fatal(err)
	}
	n := uuid.NewV7()
	if err := s.SaveAddress(ctx, &a, n); err != nil {
		t.Fatal(err)
	}
	retry := stale
	if err := retry.Edit(domain.AddressDraft{Name: "Minh", Phone: "0987654321", Street: "1 Lê Lợi", CommuneCode: "00004"}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAddress(ctx, &retry, n); err != nil {
		t.Fatalf("a retry with the same write ID: %v", err)
	}
	if err := s.SaveAddress(ctx, &stale, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a stale save: %v", err)
	}
	if err := s.SaveAddress(ctx, &a, uuid.NewV7()); err != nil {
		t.Fatalf("a second save in a row: %v", err)
	}
	got, err := s.GetAddress(ctx, me, a.ID())
	if err != nil || got.Name() != "Minh" || got.Phone() != "+84987654321" || got.WriteID() != a.WriteID() || !got.IsDefault() {
		t.Fatalf("%+v %v", got, err)
	}
	// Moving the default is a write too: a save read before it conflicts.
	other := insert(t, s, me, 1)[0]
	if err = s.SetDefaultAddress(ctx, me, other.ID()); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveAddress(ctx, &a, uuid.NewV7()); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("a save read before the default moved: %v", err)
	}
}

func TestAddressLimit(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me := seed(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, domain.MaxAddresses+5)
	for range domain.MaxAddresses + 5 {
		wg.Go(func() {
			a := address(t, me)
			errs <- s.InsertAddress(ctx, uuid.NewV7(), &a)
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
	rows, err := s.ListAddresses(ctx, me)
	if err != nil || len(rows) != domain.MaxAddresses || refused != 5 {
		t.Fatalf("%d stored, %d refused: %v", len(rows), refused, err)
	}
	if d := defaults(t, s, me); len(d) != 1 {
		t.Fatalf("defaults %v", d)
	}
}

func TestSetDefaultAddress(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	as := insert(t, s, me, 3)
	for range 2 {
		if err := s.SetDefaultAddress(ctx, me, as[1].ID()); err != nil {
			t.Fatal(err)
		}
	}
	if d := defaults(t, s, me); len(d) != 1 || d[0] != as[1].ID() {
		t.Fatalf("defaults %v", d)
	}
	if err := s.SetDefaultAddress(ctx, other, as[2].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if err := s.SetDefaultAddress(ctx, me, uuid.NewV7()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("an unknown one: %v", err)
	}
	if d := defaults(t, s, me); len(d) != 1 || d[0] != as[1].ID() {
		t.Fatalf("a refused move changed the default: %v", d)
	}
}

func TestDeleteAddressMovesTheDefault(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	as := insert(t, s, me, 3)
	if err := s.DeleteAddress(ctx, other, as[0].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("someone else's: %v", err)
	}
	if err := s.DeleteAddress(ctx, me, as[1].ID()); err != nil {
		t.Fatal(err)
	}
	if d := defaults(t, s, me); len(d) != 1 || d[0] != as[0].ID() {
		t.Fatalf("deleting another address moved the default: %v", d)
	}
	if err := s.DeleteAddress(ctx, me, as[0].ID()); err != nil {
		t.Fatal(err)
	}
	if d := defaults(t, s, me); len(d) != 1 || d[0] != as[2].ID() {
		t.Fatalf("the newest remaining is the default: %v", d)
	}
	if err := s.DeleteAddress(ctx, me, as[0].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleting again: %v", err)
	}
	if _, err := s.GetAddress(ctx, me, as[0].ID()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("the row is gone: %v", err)
	}
	if err := s.DeleteAddress(ctx, me, as[2].ID()); err != nil {
		t.Fatal(err)
	}
	if fresh := insert(t, s, me, 1)[0]; !fresh.IsDefault() {
		t.Fatal("the first address after emptying the book is the default")
	}
}

// TestAddressRequestKey: a request key the account used already returns
// the address that request added, at the cap too, and adds nothing.
func TestAddressRequestKey(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	me, other := seed(t, s), seed(t, s)
	key := uuid.NewV7()
	first := address(t, me)
	if err := s.InsertAddress(ctx, key, &first); err != nil {
		t.Fatal(err)
	}
	insert(t, s, me, domain.MaxAddresses-1)
	again := address(t, me)
	if err := s.InsertAddress(ctx, key, &again); err != nil || again.ID() != first.ID() || !again.IsDefault() {
		t.Fatalf("retry: %v %v %v", err, again.ID(), again.IsDefault())
	}
	if rows, err := s.ListAddresses(ctx, me); err != nil || len(rows) != domain.MaxAddresses {
		t.Fatalf("%d stored: %v", len(rows), err)
	}
	theirs := address(t, other)
	if err := s.InsertAddress(ctx, key, &theirs); err != nil || theirs.ID() == first.ID() {
		t.Fatalf("another account's key: %v", err)
	}
}

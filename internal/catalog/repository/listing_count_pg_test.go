package repository_test

import (
	"context"
	"maps"
	"testing"
	"uuid"

	"github.com/shopnexus/server2/internal/catalog/domain"
	"github.com/shopnexus/server2/internal/catalog/repository"
	"github.com/shopnexus/server2/internal/platform/postgres/pgtest"
)

// CountByStatus counts the seller's listings in each status and no one
// else's.
func TestCountByStatus(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	s := repository.NewListings(db)
	ctx := context.Background()
	seller, other := newAccount(t, db), newAccount(t, db)
	put := func(seller uuid.UUID, change func(l *domain.Listing)) {
		t.Helper()
		l := listing(t, seller)
		if err := s.Insert(ctx, l); err != nil {
			t.Fatal(err)
		}
		if change == nil {
			return
		}
		l.Published(map[uuid.UUID]domain.PhotoKeys{}, now)
		change(&l)
		if err := s.Save(ctx, &l, uuid.NewV7()); err != nil {
			t.Fatal(err)
		}
	}
	put(seller, nil)
	put(seller, func(*domain.Listing) {})
	put(seller, func(l *domain.Listing) { _, _ = l.Reserve(uuid.NewV7(), now) })
	put(seller, func(l *domain.Listing) { _ = l.MarkSold(now) })
	put(seller, func(l *domain.Listing) { _ = l.MarkSold(now) })
	put(other, func(*domain.Listing) {})
	got, err := s.CountByStatus(ctx, seller)
	want := map[domain.Status]int{domain.StatusProcessing: 1, domain.StatusActive: 1, domain.StatusReserved: 1, domain.StatusSold: 2}
	if err != nil || !maps.Equal(got, want) {
		t.Fatalf("%v %v", got, err)
	}
	if got, err = s.CountByStatus(ctx, uuid.NewV7()); err != nil || len(got) != 0 {
		t.Fatalf("no listings: %v %v", got, err)
	}
}

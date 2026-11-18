package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
	"github.com/shopnexus/server/internal/trust/service"
)

func TestOwnReviewUsesOriginalWindowAndMutationBoundary(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, order := e.shipped(t, buyer, seller)
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	repo := repository.NewReviews(e.db)
	trust := service.New(service.Deps{Reviews: repo, Now: func() time.Time { return now }})
	own := func(who uuid.UUID) (service.OwnReview, error) {
		m, err := trust.OwnReviews(ctx, who, []uuid.UUID{order})
		return m[order], err
	}
	none, err := own(buyer)
	if err != nil || none.State != "none" || none.CreatedAt != nil || none.ChangeUntil != nil || none.Locked {
		t.Fatalf("none: %+v %v", none, err)
	}
	facts, _, err := e.deals.Order(ctx, order)
	if err != nil {
		t.Fatal(err)
	}
	r, err := domain.NewReview(uuid.NewV7(), order, seller, buyer, facts.ListingID, 5, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Insert(ctx, r); err != nil {
		t.Fatal(err)
	}
	first := now
	now = now.Add(domain.EditWindow)
	active, err := own(buyer)
	if err != nil || active.Locked || active.State != "active" || *active.Text != "" {
		t.Fatalf("boundary: %+v %v", active, err)
	}
	if err = r.Delete(now); err != nil {
		t.Fatal(err)
	}
	if err = repo.Save(ctx, &r, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Nanosecond)
	deleted, err := own(buyer)
	if err != nil || deleted.State != "deleted" || !deleted.Locked || deleted.Stars != nil || deleted.Text != nil || !deleted.CreatedAt.Equal(first) || !errors.Is(r.Edit(4, "", now), domain.ErrLocked) {
		t.Fatalf("deleted: %+v %v", deleted, err)
	}
	// The review is not the seller's: for them the order has none.
	if other, otherErr := own(seller); otherErr != nil || other.State != "none" || other.ID != nil {
		t.Fatalf("seller: %+v %v", other, otherErr)
	}
}

// countedReviews counts the batch reads of the reviews it wraps.
type countedReviews struct {
	repository.Reviews

	byOrders atomic.Int32
}

func (c *countedReviews) ByOrders(ctx context.Context, buyer uuid.UUID, orders []uuid.UUID) ([]domain.Review, error) {
	c.byOrders.Add(1)
	return c.Reviews.ByOrders(ctx, buyer, orders)
}

func TestOwnReviewsReadsEveryOrderInOneQuery(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	seller, buyer := e.person(t), e.person(t)
	_, reviewed := e.shipped(t, buyer, seller)
	_, bare := e.shipped(t, buyer, seller)
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	repo := &countedReviews{Reviews: repository.NewReviews(e.db)}
	trust := service.New(service.Deps{Reviews: repo, Now: func() time.Time { return now }})
	facts, _, err := e.deals.Order(ctx, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	r, err := domain.NewReview(uuid.NewV7(), reviewed, seller, buyer, facts.ListingID, 4, "Ổn", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Insert(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := trust.OwnReviews(ctx, buyer, []uuid.UUID{reviewed, bare})
	if err != nil || len(got) != 2 || repo.byOrders.Load() != 1 {
		t.Fatalf("%+v %v, %d reads", got, err, repo.byOrders.Load())
	}
	if a := got[reviewed]; a.State != "active" || *a.ID != r.ID() || *a.Stars != 4 || *a.Text != "Ổn" || !a.ChangeUntil.Equal(now.Add(domain.EditWindow)) || a.Locked {
		t.Fatalf("reviewed: %+v", a)
	}
	if b := got[bare]; b.State != "none" || b.ID != nil || b.Locked {
		t.Fatalf("bare: %+v", b)
	}
	if empty, emptyErr := trust.OwnReviews(ctx, buyer, nil); emptyErr != nil || len(empty) != 0 {
		t.Fatalf("no orders: %+v %v", empty, emptyErr)
	}
	// Another buyer's review never loads: each order reads none.
	others, err := trust.OwnReviews(ctx, seller, []uuid.UUID{bare, reviewed})
	if err != nil || len(others) != 2 || others[bare].State != "none" || others[reviewed].State != "none" || others[reviewed].ID != nil {
		t.Fatalf("seller: %+v %v", others, err)
	}
}

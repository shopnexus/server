package service_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
	"github.com/shopnexus/server2/internal/trust/service"
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
	none, err := trust.OwnReview(ctx, buyer, order)
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
	active, err := trust.OwnReview(ctx, buyer, order)
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
	deleted, err := trust.OwnReview(ctx, buyer, order)
	if err != nil || deleted.State != "deleted" || !deleted.Locked || deleted.Stars != nil || deleted.Text != nil || !deleted.CreatedAt.Equal(first) || !errors.Is(r.Edit(4, "", now), domain.ErrLocked) {
		t.Fatalf("deleted: %+v %v", deleted, err)
	}
	if _, err = trust.OwnReview(ctx, seller, order); !errors.Is(err, domain.ErrOrderNotFound) {
		t.Fatalf("seller: %v", err)
	}
}

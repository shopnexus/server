package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	dealdomain "github.com/shopnexus/server2/internal/deal/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
)

// reviewable are the order statuses a review may be given in: the parcel
// went to the carrier.
var reviewable = []dealdomain.OrderStatus{dealdomain.OrderShipped, dealdomain.OrderDelivered, dealdomain.OrderReturned}

// PutReview checks that the caller may write, bought the order and the
// order is shipped, then creates the review or edits it within 7 days.
func (s *Trust) PutReview(ctx context.Context, buyer, order uuid.UUID, stars int, text string) (ReviewView, error) {
	st, err := s.accounts.Status(ctx, buyer)
	if err != nil {
		return ReviewView{}, fmt.Errorf("read account status: %w", err)
	}
	if st == accountdomain.StatusSuspended {
		return ReviewView{}, accountdomain.ErrSuspended
	}
	o, err := s.buyersOrder(ctx, buyer, order)
	if err != nil {
		return ReviewView{}, err
	}
	if !slices.Contains(reviewable, o.Status) {
		return ReviewView{}, domain.ErrNotShipped
	}
	now := s.now()
	r, err := s.reviews.ByOrder(ctx, order)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		if r, err = domain.NewReview(uuid.NewV7(), order, o.SellerID, buyer, o.ListingID, stars, text, now); err != nil {
			return ReviewView{}, err
		}
		err = s.reviews.Insert(ctx, r)
	case err != nil:
		return ReviewView{}, fmt.Errorf("get review: %w", err)
	default:
		if err = r.Edit(stars, text, now); err != nil {
			return ReviewView{}, err
		}
		err = s.reviews.Save(ctx, &r, uuid.NewV7())
	}
	if errors.Is(err, repository.ErrConflict) {
		return ReviewView{}, apperr.ErrTryAgain
	}
	if err != nil {
		return ReviewView{}, fmt.Errorf("save review: %w", err)
	}
	views, err := s.views(ctx, []domain.Review{r})
	if err != nil {
		return ReviewView{}, err
	}
	return views[0], nil
}

// DeleteReview takes the buyer's review of an order down within 7 days of
// its first submission; a review posted again later keeps that start.
func (s *Trust) DeleteReview(ctx context.Context, buyer, order uuid.UUID) error {
	if _, err := s.buyersOrder(ctx, buyer, order); err != nil {
		return err
	}
	r, err := s.reviews.ByOrder(ctx, order)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && r.Deleted()) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get review: %w", err)
	}
	if err = r.Delete(s.now()); err != nil {
		return err
	}
	err = s.reviews.Save(ctx, &r, uuid.NewV7())
	if errors.Is(err, repository.ErrConflict) {
		return apperr.ErrTryAgain
	}
	if err != nil {
		return fmt.Errorf("delete review: %w", err)
	}
	return nil
}

// SellerReviews reads one page of a seller's reviews with the average and
// count over all of them.
func (s *Trust) SellerReviews(ctx context.Context, seller uuid.UUID, p page.Request) (SellerReviews, error) {
	if _, err := s.accounts.Get(ctx, seller); err != nil {
		return SellerReviews{}, fmt.Errorf("read the seller: %w", err)
	}
	average, count, err := s.reviews.Summary(ctx, seller)
	if err != nil {
		return SellerReviews{}, fmt.Errorf("summarize reviews: %w", err)
	}
	rows, err := s.reviews.OfSeller(ctx, seller, p)
	if err != nil {
		return SellerReviews{}, fmt.Errorf("list reviews: %w", err)
	}
	views, err := s.views(ctx, rows)
	if err != nil {
		return SellerReviews{}, err
	}
	pg := page.Of(views, p, func(v ReviewView) uuid.UUID { return v.ID })
	return SellerReviews{Average: average, Count: count, Items: pg.Items, NextCursor: pg.NextCursor}, nil
}

// buyersOrder reads an order the caller bought; any other is
// domain.ErrOrderNotFound, so an order's existence does not leak.
func (s *Trust) buyersOrder(ctx context.Context, buyer, order uuid.UUID) (dealsvc.OrderFacts, error) {
	o, err := s.deals.Order(ctx, order)
	if errors.Is(err, dealdomain.ErrOrderNotFound) {
		return dealsvc.OrderFacts{}, domain.ErrOrderNotFound
	}
	if err != nil {
		return dealsvc.OrderFacts{}, fmt.Errorf("get order: %w", err)
	}
	if o.BuyerID != buyer {
		return dealsvc.OrderFacts{}, domain.ErrOrderNotFound
	}
	return o, nil
}

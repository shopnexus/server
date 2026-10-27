package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/trust/repository"
)

// ReviewFacts is a review as its notification reads it.
type ReviewFacts struct {
	ID, SellerID, ListingID uuid.UUID
	BuyerName, ListingTitle string
	Stars                   int
}

func (s *Trust) ReviewFacts(ctx context.Context, id uuid.UUID) (ReviewFacts, bool, error) {
	r, err := s.reviews.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ReviewFacts{}, false, nil
	}
	if err != nil {
		return ReviewFacts{}, false, fmt.Errorf("read review: %w", err)
	}
	buyer, err := s.accounts.Get(ctx, r.BuyerID())
	if err != nil {
		return ReviewFacts{}, false, fmt.Errorf("read buyer: %w", err)
	}
	listings, err := s.catalog.Summaries(ctx, []uuid.UUID{r.ListingID()})
	if err != nil {
		return ReviewFacts{}, false, fmt.Errorf("read listing: %w", err)
	}
	return ReviewFacts{
		ID: id, SellerID: r.SellerID(), ListingID: r.ListingID(), BuyerName: buyer.DisplayName,
		ListingTitle: listings[r.ListingID()].Title, Stars: r.Stars(),
	}, true, nil
}

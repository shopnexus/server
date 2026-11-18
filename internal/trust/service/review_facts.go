package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/trust/repository"
)

// ReviewFacts is a review as its notification reads it.
type ReviewFacts struct {
	ID, SellerID            uuid.UUID
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
	listing, _, err := s.catalog.Summary(ctx, r.ListingID())
	if err != nil {
		return ReviewFacts{}, false, fmt.Errorf("read listing: %w", err)
	}
	return ReviewFacts{
		ID: id, SellerID: r.SellerID(), BuyerName: buyer.DisplayName,
		ListingTitle: listing.Title, Stars: r.Stars(),
	}, true, nil
}

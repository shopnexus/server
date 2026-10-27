package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/trust/domain"
)

// Person is the buyer of a review.
type Person struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
}

// ListingRef is the listing a review's order bought.
type ListingRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// ReviewView is the Review schema in api/schemas/trust.yaml.
type ReviewView struct {
	ID        uuid.UUID  `json:"id"`
	Stars     int        `json:"stars"`
	Text      string     `json:"text"`
	Buyer     Person     `json:"buyer"`
	Listing   ListingRef `json:"listing"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// SellerReviews is the SellerReviews schema in api/schemas/trust.yaml.
type SellerReviews struct {
	Average    float64      `json:"average"`
	Count      int          `json:"count"`
	Items      []ReviewView `json:"items"`
	NextCursor *uuid.UUID   `json:"next_cursor,omitempty"`
}

// views builds the views of reviews, reading buyer names one by one and
// listing titles in one call.
func (s *Trust) views(ctx context.Context, reviews []domain.Review) ([]ReviewView, error) {
	ids := make([]uuid.UUID, 0, len(reviews))
	for i := range reviews {
		ids = append(ids, reviews[i].ListingID())
	}
	listings, err := s.catalog.Summaries(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	out := make([]ReviewView, 0, len(reviews))
	for i := range reviews {
		r := &reviews[i]
		buyer, getErr := s.accounts.Get(ctx, r.BuyerID())
		if getErr != nil {
			return nil, fmt.Errorf("read the buyer: %w", getErr)
		}
		out = append(out, ReviewView{
			ID: r.ID(), Stars: r.Stars(), Text: r.Text(),
			Buyer:     Person{ID: r.BuyerID(), DisplayName: buyer.DisplayName},
			Listing:   ListingRef{ID: r.ListingID(), Title: listings[r.ListingID()].Title},
			CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
		})
	}
	return out, nil
}

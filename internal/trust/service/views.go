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
	AvatarURL   string    `json:"avatar_url,omitempty"`
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

// Rating is the Rating schema in api/schemas/trust.yaml: the average stars
// and the count over a seller's reviews.
type Rating struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}

// views builds the views of reviews, reading their buyers in one call and
// their listing titles in another.
func (s *Trust) views(ctx context.Context, reviews []domain.Review) ([]ReviewView, error) {
	listingIDs, buyerIDs := make([]uuid.UUID, 0, len(reviews)), make([]uuid.UUID, 0, len(reviews))
	for i := range reviews {
		listingIDs, buyerIDs = append(listingIDs, reviews[i].ListingID()), append(buyerIDs, reviews[i].BuyerID())
	}
	listings, err := s.catalog.Summaries(ctx, listingIDs)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	buyers, err := s.accounts.GetMany(ctx, buyerIDs)
	if err != nil {
		return nil, fmt.Errorf("read the buyers: %w", err)
	}
	out := make([]ReviewView, 0, len(reviews))
	for i := range reviews {
		r := &reviews[i]
		buyer := buyers[r.BuyerID()]
		out = append(out, ReviewView{
			ID: r.ID(), Stars: r.Stars(), Text: r.Text(),
			Buyer:     Person{ID: r.BuyerID(), DisplayName: buyer.DisplayName, AvatarURL: buyer.AvatarURL},
			Listing:   ListingRef{ID: r.ListingID(), Title: listings[r.ListingID()].Title},
			CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
		})
	}
	return out, nil
}

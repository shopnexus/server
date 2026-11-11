package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"

	"github.com/shopnexus/server2/internal/trust/domain"
)

// ReportInput is what POST /v1/reports carries: exactly one of ListingID
// and AccountID.
type ReportInput struct {
	ListingID uuid.UUID
	AccountID uuid.UUID
	// ConversationID, with AccountID only, is the reporter's conversation
	// with the account.
	ConversationID uuid.UUID
	Reason         domain.Reason
	Note           string
}

// ReportView is the Report schema in api/schemas/trust.yaml. ListingID is
// zero for a report of an account.
type ReportView struct {
	ID             uuid.UUID     `json:"id"`
	ListingID      uuid.UUID     `json:"listing_id,omitzero"`
	AccountID      uuid.UUID     `json:"account_id"`
	ConversationID uuid.UUID     `json:"conversation_id,omitzero"`
	Reason         domain.Reason `json:"reason"`
	Note           string        `json:"note"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// Person is the buyer of a review.
type Person struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	// Deleted is a buyer who deleted their account; the name is empty.
	Deleted bool `json:"deleted"`
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
			Buyer: Person{
				ID: r.BuyerID(), DisplayName: buyer.DisplayName, AvatarURL: buyer.AvatarURL,
				Deleted: buyer.Status == accountdomain.StatusDeleted,
			},
			Listing:   ListingRef{ID: r.ListingID(), Title: listings[r.ListingID()].Title},
			CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
		})
	}
	return out, nil
}

// ScamWarningView is the ScamWarning schema in api/schemas/trust.yaml.
type ScamWarningView struct {
	Signals   []ScamSignalView `json:"signals"`
	Dismissed bool             `json:"dismissed"`
}

// ScamSignalView is the ScamSignal schema.
type ScamSignalView struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

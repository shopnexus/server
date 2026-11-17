package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	catalogsvc "github.com/shopnexus/server2/internal/catalog/service"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
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

// CaseList is the ReportCaseList schema in api/schemas/console.yaml. Total,
// the number of open cases, is set for the open list only.
type CaseList struct {
	page.Page[ReportCase]

	Total *int `json:"total,omitempty"`
}

// ReportCase is the ReportCase schema: one case in the console's queue.
// Listing is the case's most reported listing, nil when only the account
// was reported.
type ReportCase struct {
	Account    accountsvc.ConsoleAccount   `json:"account"`
	Reports    int                         `json:"reports"`
	Reasons    map[domain.Reason]int       `json:"reasons"`
	Listing    *catalogsvc.ReportedListing `json:"listing,omitempty"`
	FirstAt    time.Time                   `json:"first_at"`
	LastAt     time.Time                   `json:"last_at"`
	Resolution *ResolutionView             `json:"resolution,omitempty"`
	// key is what the list pages by.
	key uuid.UUID
}

// ResolutionView is the ReportResolution schema.
type ResolutionView struct {
	Outcome domain.Outcome `json:"outcome"`
	At      time.Time      `json:"at"`
	By      Resolver       `json:"by"`
}

// Resolver is the console member who resolved a case.
type Resolver struct {
	MemberID    uuid.UUID `json:"member_id"`
	DisplayName string    `json:"display_name"`
}

// CaseDetail is the ReportCaseDetail schema: a case with every listing
// reported in it, most reported first, and its reports, newest first.
type CaseDetail struct {
	Account    accountsvc.ConsoleAccount    `json:"account"`
	Reasons    map[domain.Reason]int        `json:"reasons"`
	FirstAt    time.Time                    `json:"first_at"`
	LastAt     time.Time                    `json:"last_at"`
	Resolution *ResolutionView              `json:"resolution,omitempty"`
	Listings   []catalogsvc.ReportedListing `json:"listings"`
	Reports    []CaseReport                 `json:"reports"`
}

// CaseReport is the ReportCaseReport schema: one report as console members
// see it.
type CaseReport struct {
	ID             uuid.UUID     `json:"id"`
	Reporter       Reporter      `json:"reporter"`
	Reason         domain.Reason `json:"reason"`
	Note           string        `json:"note"`
	ListingID      uuid.UUID     `json:"listing_id,omitzero"`
	ConversationID uuid.UUID     `json:"conversation_id,omitzero"`
	CreatedAt      time.Time     `json:"created_at"`
}

// Reporter is who made a report; a deleted account's name is empty.
type Reporter struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
}

func resolutionOf(r *repository.Resolution) *ResolutionView {
	if r == nil {
		return nil
	}
	return &ResolutionView{Outcome: r.Outcome, At: r.At, By: Resolver{MemberID: r.MemberID, DisplayName: r.MemberName}}
}

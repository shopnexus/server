package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
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
	// Status is open, withdrawn or resolved.
	Status    domain.ReportStatus `json:"status"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

func reportView(r domain.Report) ReportView {
	return ReportView{
		ID: r.ID(), ListingID: r.ListingID(), AccountID: r.AccountID(), ConversationID: r.ConversationID(), Reason: r.Reason(), Note: r.Note(),
		Status: r.Status(), CreatedAt: r.CreatedAt(), UpdatedAt: r.UpdatedAt(),
	}
}

// Person is the reviewer of a review.
type Person struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	// Deleted is a person who deleted their account; the name is empty.
	Deleted bool `json:"deleted"`
}

// ListingRef is the listing a review's order bought.
type ListingRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// ReviewView is the Review schema in api/schemas/trust.yaml.
type ReviewView struct {
	ID        uuid.UUID   `json:"id"`
	Role      domain.Role `json:"role"`
	Stars     int         `json:"stars"`
	Text      string      `json:"text"`
	Reviewer  Person      `json:"reviewer"`
	Listing   ListingRef  `json:"listing"`
	Reply     *Reply      `json:"reply"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// Reply is the ReviewReply schema: the subject's one answer.
type Reply struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Rating is the Rating schema in api/schemas/trust.yaml: the average stars
// and the count over a person's visible reviews in one role.
type Rating struct {
	Average float64 `json:"average"`
	Count   int     `json:"count"`
}

// views builds the views of reviews, reading their reviewers in one call
// and their listing titles in another.
func (s *Trust) views(ctx context.Context, reviews []domain.Review) ([]ReviewView, error) {
	listingIDs, reviewerIDs := make([]uuid.UUID, 0, len(reviews)), make([]uuid.UUID, 0, len(reviews))
	for i := range reviews {
		listingIDs, reviewerIDs = append(listingIDs, reviews[i].ListingID()), append(reviewerIDs, reviews[i].ReviewerID())
	}
	listings, err := s.catalog.Summaries(ctx, listingIDs)
	if err != nil {
		return nil, fmt.Errorf("read listings: %w", err)
	}
	reviewers, err := s.accounts.GetMany(ctx, reviewerIDs)
	if err != nil {
		return nil, fmt.Errorf("read the reviewers: %w", err)
	}
	out := make([]ReviewView, 0, len(reviews))
	for i := range reviews {
		r := &reviews[i]
		reviewer := reviewers[r.ReviewerID()]
		out = append(out, ReviewView{
			ID: r.ID(), Role: r.Role(), Stars: r.Stars(), Text: r.Text(),
			Reviewer: Person{
				ID: r.ReviewerID(), DisplayName: reviewer.DisplayName, AvatarURL: reviewer.AvatarURL,
				Deleted: reviewer.Status == accountdomain.StatusDeleted,
			},
			Listing:   ListingRef{ID: r.ListingID(), Title: listings[r.ListingID()].Title},
			Reply:     replyOf(r),
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

package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
	"github.com/shopnexus/server/internal/trust/repository"
)

// ReviewFacts is a review as its notifications read it.
type ReviewFacts struct {
	ID, OrderID, ReviewerID, SubjectID uuid.UUID
	Role                               domain.Role
	ReviewerName, SubjectName          string
	ListingTitle                       string
	Stars                              int
	VisibleAt                          time.Time
	// HiddenFor is how long the review stays hidden; 0 once it shows.
	HiddenFor time.Duration
	// Revealed is the other side's review this one made visible at its
	// first submission, and RevealedBy the one that made this visible;
	// zero when none.
	Revealed, RevealedBy uuid.UUID
}

// ReviewFacts reads a review for its notifications; a deleted or unknown
// one is false.
func (s *Trust) ReviewFacts(ctx context.Context, id uuid.UUID) (ReviewFacts, bool, error) {
	r, err := s.reviews.Get(ctx, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && r.Deleted()) {
		return ReviewFacts{}, false, nil
	}
	if err != nil {
		return ReviewFacts{}, false, fmt.Errorf("read review: %w", err)
	}
	people, err := s.accounts.GetMany(ctx, []uuid.UUID{r.ReviewerID(), r.SubjectID()})
	if err != nil {
		return ReviewFacts{}, false, fmt.Errorf("read the people: %w", err)
	}
	listing, _, err := s.catalog.Summary(ctx, r.ListingID())
	if err != nil {
		return ReviewFacts{}, false, fmt.Errorf("read listing: %w", err)
	}
	out := ReviewFacts{
		ID: id, OrderID: r.OrderID(), ReviewerID: r.ReviewerID(), SubjectID: r.SubjectID(), Role: r.Role(),
		ReviewerName: people[r.ReviewerID()].DisplayName, SubjectName: people[r.SubjectID()].DisplayName,
		ListingTitle: listing.Title, Stars: r.Stars(), VisibleAt: r.VisibleAt(),
	}
	if now := s.now(); !r.Visible(now) {
		out.HiddenFor = r.VisibleAt().Sub(now)
	}
	other, err := s.reviews.ByOrder(ctx, r.OrderID(), r.Role().Other())
	switch {
	case errors.Is(err, repository.ErrNotFound):
	case err != nil:
		return ReviewFacts{}, false, fmt.Errorf("read the other review: %w", err)
	case other.Deleted():
	case other.CreatedAt().Before(r.CreatedAt()) && other.VisibleAt().Equal(r.CreatedAt()):
		out.Revealed = other.ID()
	case r.CreatedAt().Before(other.CreatedAt()) && r.VisibleAt().Equal(other.CreatedAt()):
		out.RevealedBy = other.ID()
	default:
		// Each review showed on its own.
	}
	return out, true, nil
}

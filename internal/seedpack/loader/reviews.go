package loader

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/seedpack"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
	trustrepo "github.com/shopnexus/server/internal/trust/repository"
)

// Sold is the sale a ReviewWriter writes reviews for, as loaded.
type Sold struct {
	Pack       string
	ListingKey string
	ListingID  uuid.UUID
	OrderID    uuid.UUID
	SellerID   uuid.UUID
	BuyerID    uuid.UUID
}

// ReviewWriter writes the reviews of one sale. The schema decides what it
// keeps: today one review of the seller by the buyer and no reply; the
// two-way reviews add the seller's review of the buyer and one reply each.
type ReviewWriter interface {
	// Keeps says how many of reviews Write stores and how many it drops.
	Keeps(reviews []seedpack.Review) (kept, dropped int)
	// Write stores the reviews it keeps, skipping those already there, and
	// returns how many it wrote.
	Write(ctx context.Context, s Sold, reviews []seedpack.Review) (int, error)
}

// SellerReviews writes the buyer's review of the seller through trust's
// Review and its repository, and drops the buyer's review and the replies,
// which today's schema has no place for.
type SellerReviews struct {
	Reviews trustrepo.Reviews
}

func (SellerReviews) Keeps(reviews []seedpack.Review) (kept, dropped int) {
	for _, r := range reviews {
		if r.Role == seedpack.RoleSeller {
			kept++
			if r.Reply != nil {
				dropped++
			}
		} else {
			dropped++
		}
	}
	return kept, dropped
}

func (w SellerReviews) Write(ctx context.Context, s Sold, reviews []seedpack.Review) (int, error) {
	n := 0
	for _, r := range reviews {
		if r.Role != seedpack.RoleSeller {
			continue
		}
		id := seedpack.ID(s.Pack, "review", s.ListingKey+"/"+r.Role, r.At)
		review, err := trustdomain.NewReview(id, s.OrderID, s.SellerID, s.BuyerID, s.ListingID, r.Stars, r.Text, r.At)
		if err != nil {
			return n, fmt.Errorf("review: %w", err)
		}
		if err = w.Reviews.Insert(ctx, review); errors.Is(err, trustrepo.ErrConflict) {
			continue // the order has its review
		} else if err != nil {
			return n, fmt.Errorf("insert review: %w", err)
		}
		n++
	}
	return n, nil
}

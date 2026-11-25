package loader

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
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
	// ClosedAt is when the order closed, which the blind window counts from.
	ClosedAt time.Time
}

// ReviewWriter writes the reviews of one sale.
type ReviewWriter interface {
	// Keeps says how many of reviews Write stores and how many it drops.
	Keeps(reviews []seedpack.Review) (kept, dropped int)
	// Write stores the reviews it keeps, skipping those already there, and
	// returns how many it wrote.
	Write(ctx context.Context, s Sold, reviews []seedpack.Review) (int, error)
}

// Reviews writes both sides' reviews and their replies through trust's
// Review and its repository, in the order they were posted, so the second
// review reveals the first as it does in the app.
type Reviews struct {
	Reviews trustrepo.Reviews
}

func (Reviews) Keeps(reviews []seedpack.Review) (kept, dropped int) { return len(reviews), 0 }

func (w Reviews) Write(ctx context.Context, s Sold, reviews []seedpack.Review) (int, error) {
	reviews = slices.Clone(reviews)
	slices.SortFunc(reviews, func(a, b seedpack.Review) int { return a.At.Compare(b.At) })
	written := make([]trustdomain.Review, 0, len(reviews))
	replies := make([]*seedpack.Reply, 0, len(reviews))
	for i, r := range reviews {
		role, reviewer, subject := trustdomain.RoleSeller, s.BuyerID, s.SellerID
		if r.Role == seedpack.RoleBuyer {
			role, reviewer, subject = trustdomain.RoleBuyer, s.SellerID, s.BuyerID
		}
		visible := trustdomain.RevealAt(s.ClosedAt, i > 0, r.At)
		id := seedpack.ID(s.Pack, "review", s.ListingKey+"/"+r.Role, r.At)
		tags := make([]trustdomain.Tag, 0, len(r.Tags))
		for _, t := range r.Tags {
			tags = append(tags, trustdomain.Tag(t))
		}
		review, err := trustdomain.NewReview(id, s.OrderID, role, reviewer, subject, s.ListingID, r.Stars, r.Text, tags, nil, visible, r.At)
		if err != nil {
			return 0, fmt.Errorf("review: %w", err)
		}
		if err = w.Reviews.Insert(ctx, review); errors.Is(err, trustrepo.ErrConflict) {
			continue // a load before wrote it, and its reply
		} else if err != nil {
			return 0, fmt.Errorf("insert review: %w", err)
		}
		written = append(written, review)
		replies = append(replies, r.Reply)
	}
	// A second review revealed the first at its own time.
	if len(written) == 2 && len(reviews) == 2 {
		st := written[0].State()
		if reviews[1].At.Before(st.VisibleAt) {
			st.VisibleAt, st.WriteID = reviews[1].At, written[1].State().WriteID
		}
		written[0] = trustdomain.RestoreReview(st)
	}
	for i := range written {
		rp := replies[i]
		if rp == nil {
			continue
		}
		r := &written[i]
		if err := r.Reply(r.SubjectID(), rp.Text, rp.At); err != nil {
			return len(written), fmt.Errorf("reply to review %s: %w", r.ID(), err)
		}
		if err := w.Reviews.Save(ctx, r, seedpack.ID(s.Pack, "review-reply", r.ID().String(), rp.At)); err != nil {
			return len(written), fmt.Errorf("save the reply to review %s: %w", r.ID(), err)
		}
	}
	return len(written), nil
}

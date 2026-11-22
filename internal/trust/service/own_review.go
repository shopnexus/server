package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
)

// OwnReview is the OwnReview schema in api/schemas/trust.yaml: the
// caller's review of the other side of an order.
type OwnReview struct {
	State       string     `json:"state"`
	ID          *uuid.UUID `json:"id"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
	ChangeUntil *time.Time `json:"change_until"`
	// VisibleAt is when the other side sees it.
	VisibleAt *time.Time    `json:"visible_at"`
	Locked    bool          `json:"locked"`
	Stars     *int          `json:"stars,omitempty"`
	Text      *string       `json:"text,omitempty"`
	Reply     *Reply        `json:"reply,omitempty"`
	Tags      []domain.Tag  `json:"tags,omitempty"`
	Photos    []ReviewPhoto `json:"photos,omitempty"`
}

// TheirReview is the TheirReview schema: the other side's review of the
// caller. A hidden one shows only when it will show.
type TheirReview struct {
	State     string     `json:"state"`
	ID        uuid.UUID  `json:"id"`
	VisibleAt time.Time  `json:"visible_at"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	Stars     *int       `json:"stars,omitempty"`
	Text      *string    `json:"text,omitempty"`
	Reply     *Reply     `json:"reply,omitempty"`
	// CanReply is a visible review without a reply.
	CanReply bool          `json:"can_reply"`
	Tags     []domain.Tag  `json:"tags,omitempty"`
	Photos   []ReviewPhoto `json:"photos,omitempty"`
}

// OrderReviews are the two reviews of one order as one party sees them.
// Their is nil when the other side has not reviewed, or deleted theirs.
type OrderReviews struct {
	Own   OwnReview
	Their *TheirReview
}

// OrderReviews reads the reviews of the orders given, in one query, as the
// party caller sees them, keyed by order. The caller must be a party to
// each order; screen checks that first.
func (s *Trust) OrderReviews(ctx context.Context, caller uuid.UUID, orders []uuid.UUID) (map[uuid.UUID]OrderReviews, error) {
	rows, err := s.reviews.OfOrders(ctx, orders)
	if err != nil {
		return nil, fmt.Errorf("read the orders' reviews: %w", err)
	}
	now := s.now()
	out := make(map[uuid.UUID]OrderReviews, len(orders))
	for _, o := range orders {
		out[o] = OrderReviews{Own: OwnReview{State: "none"}}
	}
	for i := range rows {
		r := &rows[i]
		v := out[r.OrderID()]
		switch {
		case r.ReviewerID() == caller:
			own := OwnReview{
				State: "deleted", ID: new(r.ID()), CreatedAt: new(r.CreatedAt()), UpdatedAt: new(r.UpdatedAt()),
				ChangeUntil: new(r.ChangeUntil()), VisibleAt: new(r.VisibleAt()),
				Locked: r.CanChange(now) != nil, Reply: replyOf(r),
			}
			if !r.Deleted() {
				own.State, own.Stars, own.Text, own.Photos = "active", new(r.Stars()), new(r.Text()), s.photoViews(r.Photos())
				own.Tags = tagsOf(r)
			}
			v.Own = own
		case r.Deleted():
			// The other side took theirs down: the caller reads none.
		default:
			their := &TheirReview{State: "hidden", ID: r.ID(), VisibleAt: r.VisibleAt()}
			if r.Visible(now) {
				their.State, their.CreatedAt, their.Stars, their.Text = "visible", new(r.CreatedAt()), new(r.Stars()), new(r.Text())
				their.Reply, their.CanReply, their.Photos = replyOf(r), !r.Replied(), s.photoViews(r.Photos())
				their.Tags = tagsOf(r)
			}
			v.Their = their
		}
		out[r.OrderID()] = v
	}
	return out, nil
}

func replyOf(r *domain.Review) *Reply {
	if !r.Replied() {
		return nil
	}
	return &Reply{Text: r.ReplyText(), At: *r.RepliedAt()}
}

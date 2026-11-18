package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
)

// OwnReview is the OwnReview schema in api/schemas/trust.yaml.
type OwnReview struct {
	State       string     `json:"state"`
	ID          *uuid.UUID `json:"id"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
	ChangeUntil *time.Time `json:"change_until"`
	Locked      bool       `json:"locked"`
	Stars       *int       `json:"stars,omitempty"`
	Text        *string    `json:"text,omitempty"`
}

// OwnReviews reads buyer's reviews of the orders given in one query, keyed
// by order. An order without one of buyer's reviews reads as none.
func (s *Trust) OwnReviews(ctx context.Context, buyer uuid.UUID, orders []uuid.UUID) (map[uuid.UUID]OwnReview, error) {
	rows, err := s.reviews.ByOrders(ctx, buyer, orders)
	if err != nil {
		return nil, fmt.Errorf("read own reviews: %w", err)
	}
	now := s.now()
	out := make(map[uuid.UUID]OwnReview, len(orders))
	for _, o := range orders {
		out[o] = OwnReview{State: "none"}
	}
	for i := range rows {
		r := &rows[i]
		v := OwnReview{
			State: "deleted", ID: new(r.ID()), CreatedAt: new(r.CreatedAt()), UpdatedAt: new(r.UpdatedAt()),
			ChangeUntil: new(r.CreatedAt().Add(domain.EditWindow)), Locked: r.CanChange(now) != nil,
		}
		if !r.Deleted() {
			v.State, v.Stars, v.Text = "active", new(r.Stars()), new(r.Text())
		}
		out[r.OrderID()] = v
	}
	return out, nil
}

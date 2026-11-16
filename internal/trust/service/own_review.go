package service

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/trust/domain"
	"github.com/shopnexus/server2/internal/trust/repository"
)

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

func (s *Trust) OwnReview(ctx context.Context, buyer, order uuid.UUID) (OwnReview, error) {
	r, err := s.reviews.ByOrder(ctx, order)
	if errors.Is(err, repository.ErrNotFound) {
		return OwnReview{State: "none"}, nil
	}
	if err != nil {
		return OwnReview{}, fmt.Errorf("read own review: %w", err)
	}
	if r.BuyerID() != buyer {
		return OwnReview{}, domain.ErrOrderNotFound
	}
	out := OwnReview{
		State: "deleted", ID: new(r.ID()), CreatedAt: new(r.CreatedAt()), UpdatedAt: new(r.UpdatedAt()),
		ChangeUntil: new(r.CreatedAt().Add(domain.EditWindow)), Locked: r.CanChange(s.now()) != nil,
	}
	if !r.Deleted() {
		out.State, out.Stars, out.Text = "active", new(r.Stars()), new(r.Text())
	}
	return out, nil
}

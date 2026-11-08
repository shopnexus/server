package service

import (
	"context"
	"fmt"
	"uuid"
)

// Rating reads the average and count over every review of the seller; a
// seller without reviews has 0 and 0.
func (s *Trust) Rating(ctx context.Context, seller uuid.UUID) (Rating, error) {
	average, count, err := s.reviews.Summary(ctx, seller)
	if err != nil {
		return Rating{}, fmt.Errorf("summarize reviews: %w", err)
	}
	return Rating{Average: average, Count: count}, nil
}

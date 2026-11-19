package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
)

// Rating reads the average and count over the visible reviews of subject
// in role; none give 0 and 0.
func (s *Trust) Rating(ctx context.Context, subject uuid.UUID, role domain.Role) (Rating, error) {
	average, count, err := s.reviews.Summary(ctx, subject, role, s.now())
	if err != nil {
		return Rating{}, fmt.Errorf("summarize reviews: %w", err)
	}
	return Rating{Average: average, Count: count}, nil
}

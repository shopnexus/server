package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
)

// Rating reads the average, count and tag counts over the visible reviews
// of subject in role; none give 0, 0 and no tags.
func (s *Trust) Rating(ctx context.Context, subject uuid.UUID, role domain.Role) (Rating, error) {
	now := s.now()
	average, count, err := s.reviews.Summary(ctx, subject, role, now)
	if err != nil {
		return Rating{}, fmt.Errorf("summarize reviews: %w", err)
	}
	counts, err := s.reviews.TagCounts(ctx, subject, role, now)
	if err != nil {
		return Rating{}, fmt.Errorf("count review tags: %w", err)
	}
	tags := make([]TagCount, 0, len(counts))
	for _, c := range counts {
		tags = append(tags, TagCount{Tag: c.Tag, Count: c.Count})
	}
	return Rating{Average: average, Count: count, Tags: tags}, nil
}

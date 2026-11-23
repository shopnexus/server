package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/feedback/domain"
)

func (s *Feedback) Facts(ctx context.Context, id uuid.UUID) (Facts, bool, error) {
	f, err := s.store.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return Facts{}, false, nil
	}
	if err != nil {
		return Facts{}, false, fmt.Errorf("read feedback: %w", err)
	}
	return Facts{ID: f.ID, Owner: f.AccountID, Status: f.Status, Version: f.ShippedVersion, Reply: f.Reply}, true, nil
}

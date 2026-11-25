package service

import (
	"context"
	"fmt"
	"uuid"
)

func (s *Account) ActiveSessions(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	active, err := s.d.Sessions.ActiveAmong(ctx, ids, s.d.Now())
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}
	return active, nil
}

package service

import (
	"context"
	"fmt"
	"uuid"
)

func (a *Auth) ActiveSessions(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	active, err := a.sessions.ActiveAmong(ctx, ids, a.now())
	if err != nil {
		return nil, fmt.Errorf("read sessions: %w", err)
	}
	return active, nil
}

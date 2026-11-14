package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/domain"
)

// ReplyRate is the ReplyRate schema in api/schemas/screen_account.yaml.
type ReplyRate struct {
	Percent int `json:"percent"`
	Count   int `json:"count"`
}

func (c *Chat) ReplyRate(ctx context.Context, account uuid.UUID) (*ReplyRate, error) {
	now := c.now()
	threads, err := c.chats.ReplyThreads(ctx, account, now.Add(-domain.ReplyPeriod))
	if err != nil {
		return nil, fmt.Errorf("read threads: %w", err)
	}
	percent, count, ok := domain.ReplyRateOf(threads, now)
	if !ok {
		return nil, nil //nolint:nilnil // Too few conversations count for a rate.
	}
	return &ReplyRate{Percent: percent, Count: count}, nil
}

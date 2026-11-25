package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
)

func (c *Chat) ReplyTime(ctx context.Context, account uuid.UUID) (domain.ReplyTime, error) {
	samples, median, err := c.d.Chats.ReplyWait(ctx, account, c.d.Now().Add(-domain.ReplyTimePeriod))
	if err != nil {
		return "", fmt.Errorf("read the reply wait: %w", err)
	}
	t, _ := domain.ReplyTimeOf(samples, median)
	return t, nil
}

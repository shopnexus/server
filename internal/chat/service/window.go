package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/repository"
)

func (c *Chat) Window(ctx context.Context, conversation, upTo uuid.UUID, n int) (Window, bool, error) {
	conv, err := c.d.Chats.Get(ctx, conversation)
	if errors.Is(err, repository.ErrNotFound) {
		return Window{}, false, nil
	}
	if err != nil {
		return Window{}, false, fmt.Errorf("get conversation: %w", err)
	}
	ms, err := c.d.Chats.Window(ctx, conversation, upTo, n)
	if err != nil {
		return Window{}, false, fmt.Errorf("read window: %w", err)
	}
	w := Window{AccountA: conv.AccountA, AccountB: conv.AccountB, Lines: make([]WindowLine, 0, len(ms))}
	for i := len(ms) - 1; i >= 0; i-- {
		w.Lines = append(w.Lines, WindowLine{ID: ms[i].ID(), SenderID: ms[i].SenderID(), Text: ms[i].Body(), ListingID: ms[i].ListingID()})
	}
	return w, true, nil
}

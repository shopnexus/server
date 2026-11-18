package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
)

// Thread is a conversation's last message and a member's unread count.
type Thread struct {
	// First is the conversation's oldest sent message; in a support chat,
	// the ticket's card.
	First  *MessageView
	Last   *MessageView
	Unread int
}

// OpenSupport makes the support chat id of account and ShopNexus, or
// returns it when a retried step made it already.
func (c *Chat) OpenSupport(ctx context.Context, account, id uuid.UUID) (ConversationView, error) {
	if _, err := c.chats.OpenSupport(ctx, domain.NewSupportConversation(id, account, c.now())); err != nil {
		return ConversationView{}, fmt.Errorf("open the support chat: %w", err)
	}
	return c.Get(ctx, account, id)
}

func (c *Chat) Threads(ctx context.Context, account uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Thread, error) {
	rows, err := c.chats.Threads(ctx, account, ids)
	if err != nil {
		return nil, fmt.Errorf("read threads: %w", err)
	}
	out := make(map[uuid.UUID]Thread, len(rows))
	for id := range rows {
		first, last := c.messageView(rows[id].First), c.messageView(rows[id].Last)
		out[id] = Thread{First: &first, Last: &last, Unread: rows[id].Unread}
	}
	return out, nil
}

func (c *Chat) SupportUnread(ctx context.Context, account uuid.UUID) (int, error) {
	n, err := c.chats.SupportUnread(ctx, account)
	if err != nil {
		return 0, fmt.Errorf("count support unread: %w", err)
	}
	return n, nil
}

package service

import (
	"context"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	"github.com/shopnexus/server2/internal/chat/domain"
)

// OpenPair opens the pair's conversation unless the two have one; the
// rules of who may open it are the caller's.
func (c *Chat) OpenPair(ctx context.Context, one, other uuid.UUID) (uuid.UUID, error) {
	conv, err := c.chats.OpenPair(ctx, domain.NewConversation(uuid.NewV7(), one, other, c.now()))
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("open conversation: %w", err)
	}
	return conv.ID, nil
}

// PostMove adds the move's message, sent at the move's time, to a pair
// conversation whose member, or ShopNexus, made the move. The message
// counts as unread for every member who did not make the move, and a
// block changes nothing: the deal goes on across it.
func (c *Chat) PostMove(ctx context.Context, in MoveInput) error {
	conv, err := c.chats.Get(ctx, in.ConversationID)
	if err != nil {
		return fmt.Errorf("get conversation %s: %w", in.ConversationID, err)
	}
	if conv.Kind != domain.KindPair || (!conv.Has(in.SenderID) && in.SenderID != accountdomain.ShopNexusID) {
		return fmt.Errorf("move %s of %s does not belong in conversation %s", in.ID, in.SenderID, conv.ID)
	}
	m := domain.NewDealMessage(in.ID, conv.ID, in.SenderID,
		domain.DealMove{DealID: in.DealID, Kind: in.Kind, By: in.By, Amount: in.Amount}, in.At)
	if err = c.chats.AddMessage(ctx, m); err != nil {
		return fmt.Errorf("add the deal message: %w", err)
	}
	return nil
}

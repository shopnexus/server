package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
)

// OpenPair opens the pair's conversation unless the two have one; the
// rules of who may open it are the caller's. The conversation is new when
// it took the ID minted here.
func (c *Chat) OpenPair(ctx context.Context, one, other uuid.UUID) (uuid.UUID, bool, error) {
	id := uuid.NewV7()
	conv, err := c.d.Chats.OpenPair(ctx, domain.NewConversation(id, one, other, c.d.Now()))
	if err != nil {
		return uuid.UUID{}, false, fmt.Errorf("open conversation: %w", err)
	}
	return conv.ID, conv.ID == id, nil
}

func (c *Chat) DropPair(ctx context.Context, id uuid.UUID) error {
	if err := c.d.Chats.DropPair(ctx, id); err != nil {
		return fmt.Errorf("drop conversation: %w", err)
	}
	return nil
}

// PostMove adds the move's message, sent at the move's time, to a pair
// conversation whose member, or ShopNexus, made the move. The message
// counts as unread for every member who did not make the move, and a
// block changes nothing: the deal goes on across it. A move that does not
// belong in the conversation is domain.ErrMisplacedMove.
func (c *Chat) PostMove(ctx context.Context, in MoveInput) error {
	conv, err := c.d.Chats.Get(ctx, in.ConversationID)
	if errors.Is(err, repository.ErrNotFound) {
		return fmt.Errorf("conversation %s: %w", in.ConversationID, domain.ErrMisplacedMove)
	}
	if err != nil {
		return fmt.Errorf("get conversation %s: %w", in.ConversationID, err)
	}
	if conv.Kind != domain.KindPair || (!conv.Has(in.SenderID) && in.SenderID != accountdomain.ShopNexusID) {
		return fmt.Errorf("move %s of %s in conversation %s: %w", in.ID, in.SenderID, conv.ID, domain.ErrMisplacedMove)
	}
	m := domain.NewDealMessage(in.ID, conv.ID, in.SenderID,
		domain.DealMove{DealID: in.DealID, Kind: in.Kind, By: in.By, Amount: in.Amount}, in.At)
	if _, err = c.d.Chats.AddMessage(ctx, m, uuid.Nil()); err != nil {
		return fmt.Errorf("add the deal message: %w", err)
	}
	return nil
}

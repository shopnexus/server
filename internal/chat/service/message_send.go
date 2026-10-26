package service

import (
	"context"
	"fmt"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/domain"
)

// Open checks the first message, runs MessageSend once per buyer and
// idempotencyKey, then reads the conversation.
func (c *Chat) Open(ctx context.Context, buyer uuid.UUID, idempotencyKey string, in OpenInput) (ConversationView, error) {
	if _, err := domain.NewMessage(uuid.UUID{}, uuid.UUID{}, buyer, in.Text, in.Photos, &in.ListingID, c.now()); err != nil {
		return ConversationView{}, err
	}
	res, err := c.send.Call(ctx, buyer.String()+"/"+idempotencyKey, SendFlowInput{
		Sender: buyer, Opening: true, ListingID: &in.ListingID, Text: in.Text, Photos: in.Photos,
	})
	if err != nil {
		return ConversationView{}, err
	}
	return c.Get(ctx, buyer, res.ConversationID)
}

// Send checks the message, runs MessageSend once per sender and
// idempotencyKey, then reads the message back.
func (c *Chat) Send(ctx context.Context, sender, conversation uuid.UUID, idempotencyKey string, in SendInput) (MessageView, error) {
	if _, err := domain.NewMessage(uuid.UUID{}, conversation, sender, in.Text, in.Photos, in.ListingID, c.now()); err != nil {
		return MessageView{}, err
	}
	res, err := c.send.Call(ctx, sender.String()+"/"+idempotencyKey, SendFlowInput{
		Sender: sender, ConversationID: conversation, ListingID: in.ListingID, Text: in.Text, Photos: in.Photos,
	})
	if err != nil {
		return MessageView{}, err
	}
	m, err := c.chats.GetMessage(ctx, res.MessageID)
	if err != nil {
		return MessageView{}, fmt.Errorf("read the message: %w", err)
	}
	return c.messageView(m), nil
}

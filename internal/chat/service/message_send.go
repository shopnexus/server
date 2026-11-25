package service

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/shopnexus/server/internal/chat/domain"
	"github.com/shopnexus/server/internal/chat/repository"
)

// Open checks the first message; a key the buyer sent a message with
// already answers that message's conversation (NFR-IDEM-01), otherwise it
// runs MessageSend. Then it reads the conversation.
func (c *Chat) Open(ctx context.Context, buyer, key uuid.UUID, in OpenInput) (ConversationView, error) {
	if _, err := domain.NewMessage(uuid.UUID{}, uuid.UUID{}, buyer, in.Text, in.Photos, &in.ListingID, nil, false, c.now()); err != nil {
		return ConversationView{}, err
	}
	m, err := c.chats.MessageByKey(ctx, buyer, key)
	if err == nil {
		return c.Get(ctx, buyer, m.ConversationID())
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return ConversationView{}, fmt.Errorf("find the key's message: %w", err)
	}
	res, err := c.send.Call(ctx, SendFlowInput{
		Sender: buyer, Key: key, Opening: true, ListingID: &in.ListingID, Text: in.Text, Photos: in.Photos,
	})
	if err != nil {
		return ConversationView{}, err
	}
	return c.Get(ctx, buyer, res.ConversationID)
}

// Send checks the message; a key the sender sent a message with already
// answers that message (NFR-IDEM-01), otherwise it runs MessageSend. Then
// it reads the message back.
func (c *Chat) Send(ctx context.Context, sender, conversation, key uuid.UUID, in SendInput) (MessageView, error) {
	if _, err := domain.NewMessage(uuid.UUID{}, conversation, sender, in.Text, in.Photos, in.ListingID, in.ReplyTo,
		in.OpensTicket, c.now()); err != nil {
		return MessageView{}, err
	}
	m, err := c.chats.MessageByKey(ctx, sender, key)
	if errors.Is(err, repository.ErrNotFound) {
		res, callErr := c.send.Call(ctx, SendFlowInput{
			Sender: sender, Key: key, ConversationID: conversation, ListingID: in.ListingID, Text: in.Text, Photos: in.Photos,
			ReplyTo: in.ReplyTo, OpensTicket: in.OpensTicket,
		})
		if callErr != nil {
			return MessageView{}, callErr
		}
		m, err = c.chats.GetMessage(ctx, res.MessageID)
	}
	if err != nil {
		return MessageView{}, fmt.Errorf("read the message: %w", err)
	}
	v := []MessageView{c.messageView(m)}
	if err = c.decorate(ctx, sender, v, []domain.Message{m}); err != nil {
		return MessageView{}, err
	}
	return v[0], nil
}

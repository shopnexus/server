package service

import (
	"context"
	"fmt"
	"uuid"
)

// Unread is the Unread schema in api/schemas/chat.yaml.
type Unread struct {
	Unread        int `json:"unread"`
	Notifications int `json:"notifications"`
	Offers        int `json:"offers"`
}

// Unread reads chat's unread total, the inbox's, then the unread notices,
// then the open offers on account's listings.
func (s *Screen) Unread(ctx context.Context, account uuid.UUID) (Unread, error) {
	messages, err := s.chats.Unread(ctx, account)
	if err != nil {
		return Unread{}, fmt.Errorf("count unread messages: %w", err)
	}
	notices, err := s.notices.Unread(ctx, account)
	if err != nil {
		return Unread{}, fmt.Errorf("count unread notices: %w", err)
	}
	offers, err := s.deals.OpenOfferCount(ctx, account)
	if err != nil {
		return Unread{}, fmt.Errorf("count open offers: %w", err)
	}
	return Unread{Unread: messages, Notifications: notices, Offers: offers}, nil
}

package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/domain"
	dealsvc "github.com/shopnexus/server2/internal/deal/service"
)

// OpenInput is the first message of a conversation, about a listing.
type OpenInput struct {
	ListingID uuid.UUID
	Text      string
	Photos    []uuid.UUID
}

// SendInput is a message as its sender writes it.
type SendInput struct {
	Text      string
	Photos    []uuid.UUID
	ListingID *uuid.UUID
}

// PersonView is the other member of a conversation.
type PersonView struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
}

// PhotoView is a photo of a message; the URLs are set once it is processed.
type PhotoView struct {
	UploadID uuid.UUID `json:"upload_id"`
	URL      string    `json:"url,omitempty"`
	ThumbURL string    `json:"thumb_url,omitempty"`
}

// MessageView is the Message schema in api/schemas/chat.yaml.
type MessageView struct {
	ID        uuid.UUID            `json:"id"`
	SenderID  uuid.UUID            `json:"sender_id"`
	Text      string               `json:"text"`
	Photos    []PhotoView          `json:"photos"`
	ListingID *uuid.UUID           `json:"listing_id,omitempty"`
	Status    domain.MessageStatus `json:"status"`
	Failure   string               `json:"failure,omitempty"`
	CreatedAt time.Time            `json:"created_at"`
}

// ConversationView is the Conversation schema in api/schemas/chat.yaml.
type ConversationView struct {
	ID       uuid.UUID          `json:"id"`
	Other    PersonView         `json:"other"`
	Deals    []dealsvc.DealView `json:"deals"`
	Archived bool               `json:"archived"`
}

// InboxItem is a conversation in the inbox.
type InboxItem struct {
	ID          uuid.UUID   `json:"id"`
	Other       PersonView  `json:"other"`
	LastMessage MessageView `json:"last_message"`
	Unread      int         `json:"unread"`
	Archived    bool        `json:"archived"`
}

// messageView shows m with its photos' URLs.
func (c *Chat) messageView(m domain.Message) MessageView {
	photos := make([]PhotoView, 0, len(m.Photos()))
	for _, p := range m.Photos() {
		v := PhotoView{UploadID: p.UploadID}
		if p.PhotoKey != "" {
			v.URL, v.ThumbURL = c.media.URL(p.PhotoKey), c.media.URL(p.ThumbKey)
		}
		photos = append(photos, v)
	}
	return MessageView{
		ID: m.ID(), SenderID: m.SenderID(), Text: m.Body(), Photos: photos, ListingID: m.ListingID(),
		Status: m.Status(), Failure: m.Failure(), CreatedAt: m.CreatedAt(),
	}
}

// person reads the display name of account.
func (c *Chat) person(ctx context.Context, account uuid.UUID) (PersonView, error) {
	a, err := c.accounts.Get(ctx, account)
	if err != nil {
		return PersonView{}, fmt.Errorf("read the other member: %w", err)
	}
	return PersonView{ID: account, DisplayName: a.DisplayName}, nil
}

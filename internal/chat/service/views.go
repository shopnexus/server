package service

import (
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server2/internal/account/domain"
	accountsvc "github.com/shopnexus/server2/internal/account/service"
	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/platform/page"
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
	ID           uuid.UUID  `json:"id"`
	DisplayName  string     `json:"display_name"`
	AvatarURL    string     `json:"avatar_url,omitempty"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	// Deleted is a person who deleted their account; the name is empty.
	Deleted bool `json:"deleted"`
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
	ID       uuid.UUID  `json:"id"`
	Other    PersonView `json:"other"`
	Archived bool       `json:"archived"`
}

// InboxItem is a conversation in the inbox.
type InboxItem struct {
	ID          uuid.UUID   `json:"id"`
	Other       PersonView  `json:"other"`
	LastMessage MessageView `json:"last_message"`
	Unread      int         `json:"unread"`
	Archived    bool        `json:"archived"`
}

// MessageHit is a message an inbox search found, with its conversation's
// other member.
type MessageHit struct {
	ConversationID uuid.UUID   `json:"conversation_id"`
	Other          PersonView  `json:"other"`
	Message        MessageView `json:"message"`
}

// SearchResult is an inbox search: the matching conversations, on the first
// page only, and a page of matching messages.
type SearchResult struct {
	Threads  []InboxItem
	Messages page.Page[MessageHit]
}

// personView shows account a as the other member id.
func personView(id uuid.UUID, a accountsvc.AccountView) PersonView {
	return PersonView{
		ID: id, DisplayName: a.DisplayName, AvatarURL: a.AvatarURL, LastActiveAt: a.LastActiveAt,
		Deleted: a.Status == accountdomain.StatusDeleted,
	}
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

// Window is a conversation's newest sent messages with text, oldest first,
// with its two members, for module trust's scam check.
type Window struct {
	AccountA, AccountB uuid.UUID
	Lines              []WindowLine
}

// WindowLine is one message of a Window.
type WindowLine struct {
	ID, SenderID uuid.UUID
	Text         string
	ListingID    *uuid.UUID
}

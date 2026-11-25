package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	accountsvc "github.com/shopnexus/server/internal/account/service"
	"github.com/shopnexus/server/internal/chat/domain"
	mediadomain "github.com/shopnexus/server/internal/media/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// OpenInput is the first message of a conversation, about a listing.
type OpenInput struct {
	ListingID uuid.UUID
	Text      string
	Photos    []uuid.UUID
}

// SendInput is a message as its sender writes it. ReplyTo is the earlier
// message it quotes. OpensTicket lets the first message of a support
// ticket carry neither text nor photos; module support sets it, and the
// HTTP binding has no such field.
type SendInput struct {
	Text        string
	Photos      []uuid.UUID
	ListingID   *uuid.UUID
	ReplyTo     *uuid.UUID
	OpensTicket bool
}

// PersonView is the other member of a conversation.
type PersonView struct {
	ID           uuid.UUID  `json:"id"`
	DisplayName  string     `json:"display_name"`
	AvatarURL    string     `json:"avatar_url,omitempty"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	// Deleted is a person who deleted their account; the name is empty.
	Deleted bool `json:"deleted"`
	// Official is ShopNexus, the other side of every support chat.
	Official bool `json:"official,omitempty"`
	// AwayUntil is set only while the person is in away mode.
	AwayUntil *time.Time `json:"away_until,omitempty"`
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
	ReplyTo   *QuoteView           `json:"reply_to,omitempty"`
	Deal      *MessageDealView     `json:"deal,omitempty"`
	Refs      []RefView            `json:"refs,omitempty"`
	Status    domain.MessageStatus `json:"status"`
	Failure   string               `json:"failure,omitempty"`
	// FailureCode is Failure as a code.
	FailureCode mediadomain.FailureCode `json:"failure_code,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
}

// MessageDealView is the move a deal message is about: the MessageDeal
// schema.
type MessageDealView struct {
	DealID uuid.UUID `json:"deal_id"`
	Move   MoveView  `json:"move"`
}

// MoveView is a move as deal's Move schema shows it; Amount is absent on a
// move without one.
type MoveView struct {
	Kind   string    `json:"kind"`
	By     string    `json:"by"`
	Amount *int64    `json:"amount,omitempty"`
	At     time.Time `json:"at"`
}

// MoveInput is a move on a deal for its deal message. ID is the move's,
// which the message takes. SenderID is the side that moved, or
// accountdomain.ShopNexusID for the system. Kind and By are deal's names;
// Amount is 0 on a move without one.
type MoveInput struct {
	ID, ConversationID, DealID, SenderID uuid.UUID
	Kind, By                             string
	Amount                               int64
	At                                   time.Time
}

// QuoteView is the message a reply quotes, as it reads now: the Quote
// schema.
type QuoteView struct {
	ID       uuid.UUID `json:"id"`
	SenderID uuid.UUID `json:"sender_id"`
	Text     string    `json:"text"`
	Photos   int       `json:"photos"`
}

// maxQuote is how much of a quoted message's text a reply carries.
const maxQuote = 200

// ConversationView is the Conversation schema in api/schemas/chat.yaml.
// OtherRead is the newest message the other member has read, nil before
// they read any.
type ConversationView struct {
	ID        uuid.UUID  `json:"id"`
	Other     PersonView `json:"other"`
	Archived  bool       `json:"archived"`
	OtherRead *uuid.UUID `json:"other_read_message_id,omitempty"`
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

// PersonFromAccount returns an account's public fields, away mode included,
// and its deleted and official flags.
func PersonFromAccount(id uuid.UUID, a accountsvc.AccountView) PersonView {
	return PersonView{
		ID: id, DisplayName: a.DisplayName, AvatarURL: a.AvatarURL, LastActiveAt: a.LastActiveAt,
		Deleted: a.Status == accountdomain.StatusDeleted, Official: id == accountdomain.ShopNexusID, AwayUntil: a.AwayUntil,
	}
}

// decorate adds what a message's own row does not hold: the message each
// one quotes and the cards of its ShopNexus links, as viewer sees them.
func (c *Chat) decorate(ctx context.Context, viewer uuid.UUID, views []MessageView, msgs []domain.Message) error {
	if err := c.quoted(ctx, views, msgs); err != nil {
		return err
	}
	return c.refs(ctx, viewer, views)
}

// quoted adds to each view the message it quotes, read now in one query; a
// quote whose message is gone is left off.
func (c *Chat) quoted(ctx context.Context, views []MessageView, msgs []domain.Message) error {
	var ids []uuid.UUID
	for i := range msgs {
		if id := msgs[i].ReplyTo(); id != nil {
			ids = append(ids, *id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	quoted, err := c.d.Chats.GetMessages(ctx, ids)
	if err != nil {
		return fmt.Errorf("read the quoted messages: %w", err)
	}
	for i := range msgs {
		id := msgs[i].ReplyTo()
		if id == nil {
			continue
		}
		q, ok := quoted[*id]
		if !ok {
			continue
		}
		text := q.Body()
		if r := []rune(text); len(r) > maxQuote {
			text = string(r[:maxQuote])
		}
		views[i].ReplyTo = &QuoteView{ID: q.ID(), SenderID: q.SenderID(), Text: text, Photos: len(q.Photos())}
	}
	return nil
}

// messageView shows m with its photos' URLs.
func (c *Chat) messageView(m domain.Message) MessageView {
	photos := make([]PhotoView, 0, len(m.Photos()))
	for _, p := range m.Photos() {
		v := PhotoView{UploadID: p.UploadID}
		if p.PhotoKey != "" {
			v.URL, v.ThumbURL = c.d.Media.URL(p.PhotoKey), c.d.Media.URL(p.ThumbKey)
		}
		photos = append(photos, v)
	}
	v := MessageView{
		ID: m.ID(), SenderID: m.SenderID(), Text: m.Body(), Photos: photos, ListingID: m.ListingID(),
		Status: m.Status(), Failure: m.Failure(), FailureCode: mediadomain.FailureCodeOf(m.Failure()), CreatedAt: m.CreatedAt(),
	}
	if d := m.Deal(); d != nil {
		v.Deal = &MessageDealView{DealID: d.DealID, Move: MoveView{Kind: d.Kind, By: d.By, At: m.CreatedAt()}}
		if d.Amount != 0 {
			v.Deal.Move.Amount = new(d.Amount)
		}
	}
	return v
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

// QuickReplies is the QuickReplies schema in api/schemas/chat.yaml.
type QuickReplies struct {
	Items []string `json:"items"`
}

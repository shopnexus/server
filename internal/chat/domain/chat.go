// Package domain holds module chat's entities: a conversation between two
// people, whatever their roles, its members' read markers and archives, and
// its messages.
package domain

import (
	"bytes"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	"github.com/shopnexus/server/internal/platform/apperr"
)

const (
	MaxText   = 1000
	MaxPhotos = 4
	// MaxQuery caps an inbox search, counted after trimming.
	MaxQuery = 100
)

type MessageStatus string

const (
	// StatusProcessing is a message whose photos media is processing; only
	// its sender sees it.
	StatusProcessing MessageStatus = "processing"
	StatusSent       MessageStatus = "sent"
	// StatusFailed is a message whose photos media refused; only its
	// sender sees it.
	StatusFailed MessageStatus = "failed"
)

// Photo is an upload of a message; the keys are set once it is processed.
type Photo struct {
	UploadID uuid.UUID `json:"upload_id"`
	PhotoKey string    `json:"photo_key,omitempty"`
	ThumbKey string    `json:"thumb_key,omitempty"`
}

// DealMove is the move on a deal that a deal message carries. Kind and By
// are module deal's names, which chat keeps as they come; Amount is 0 on a
// move without one.
type DealMove struct {
	DealID uuid.UUID `json:"deal_id"`
	Kind   string    `json:"kind"`
	By     string    `json:"by"`
	Amount int64     `json:"amount,omitempty"`
}

// Message is one message of a conversation. ListingID names the listing it
// is about, if any; replyTo the earlier message it quotes. A deal message
// carries deal, the move the server wrote it for, and nothing else.
type Message struct{ s MessageState }

// NewMessage checks a message as its sender wrote it: text 0 to 1,000
// characters after trimming and 0 to 4 photos, at least one of the two,
// unless the message opens a support ticket, whose card may carry neither.
// Whether replyTo is a sent message of the conversation is the send flow's
// check.
func NewMessage(id, conversation, sender uuid.UUID, body string, photos []uuid.UUID, listing, replyTo *uuid.UUID,
	opensTicket bool, now time.Time,
) (Message, error) {
	e := ErrInvalidMessage
	body = strings.TrimSpace(body)
	n := utf8.RuneCountInString(body)
	if n > MaxText {
		e = e.WithField("text", apperr.TooLong, "1 to 1,000 characters, or photos")
	} else if n == 0 && len(photos) == 0 && !opensTicket {
		e = e.WithField("text", apperr.Required, "1 to 1,000 characters, or photos")
	}
	sorted := slices.SortedFunc(slices.Values(photos), func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	if len(photos) > MaxPhotos {
		e = e.WithField("photos", apperr.TooMany, "up to 4 uploads, each once")
	} else if len(slices.Compact(sorted)) != len(photos) {
		e = e.WithField("photos", apperr.Duplicate, "up to 4 uploads, each once")
	}
	if len(e.Fields) > 0 {
		return Message{}, e
	}
	m := Message{s: MessageState{
		ID: id, ConversationID: conversation, SenderID: sender, Body: body, ListingID: listing, ReplyTo: replyTo,
		Status: StatusSent, CreatedAt: now,
	}}
	for _, p := range photos {
		m.s.Photos = append(m.s.Photos, Photo{UploadID: p})
	}
	if len(photos) > 0 {
		m.s.Status = StatusProcessing
	}
	return m, nil
}

// NewDealMessage is the message of a move on a deal, sent at the move's
// time: id is the move's, so the same move writes one message. sender is
// the side that moved, or ShopNexus for a move by the system.
func NewDealMessage(id, conversation, sender uuid.UUID, move DealMove, at time.Time) Message {
	return Message{s: MessageState{
		ID: id, ConversationID: conversation, SenderID: sender, Deal: &move, Status: StatusSent, CreatedAt: at,
	}}
}

// Published records the processed photos; the message is sent.
func (m *Message) Published(keys map[uuid.UUID]Photo) {
	photos := make([]Photo, len(m.s.Photos))
	for i, p := range m.s.Photos {
		photos[i] = keys[p.UploadID]
		photos[i].UploadID = p.UploadID
	}
	m.s.Photos, m.s.Status = photos, StatusSent
}

// Failed records why media refused a photo.
func (m *Message) Failed(reason string) { m.s.Status, m.s.Failure = StatusFailed, reason }

// VisibleTo reports whether account may read the message: its sender
// always, the other member once it is sent.
func (m *Message) VisibleTo(account uuid.UUID) bool {
	return m.s.Status == StatusSent || account == m.s.SenderID
}

func (m *Message) ID() uuid.UUID             { return m.s.ID }
func (m *Message) ConversationID() uuid.UUID { return m.s.ConversationID }
func (m *Message) SenderID() uuid.UUID       { return m.s.SenderID }
func (m *Message) Body() string              { return m.s.Body }
func (m *Message) Photos() []Photo           { return append([]Photo{}, m.s.Photos...) }
func (m *Message) ListingID() *uuid.UUID     { return m.s.ListingID }
func (m *Message) ReplyTo() *uuid.UUID       { return m.s.ReplyTo }

// Deal returns a copy of the move a deal message carries, nil on any other
// message.
func (m *Message) Deal() *DealMove {
	if m.s.Deal == nil {
		return nil
	}
	return new(*m.s.Deal)
}

func (m *Message) Status() MessageStatus { return m.s.Status }
func (m *Message) Failure() string       { return m.s.Failure }
func (m *Message) CreatedAt() time.Time  { return m.s.CreatedAt }

// UploadIDs returns the message's uploads in order.
func (m *Message) UploadIDs() []uuid.UUID {
	out := make([]uuid.UUID, len(m.s.Photos))
	for i, p := range m.s.Photos {
		out[i] = p.UploadID
	}
	return out
}

// MessageState is every stored field of a message. Stores and the flows
// that journal a message only.
type MessageState struct {
	ID             uuid.UUID     `json:"id"`
	ConversationID uuid.UUID     `json:"conversation_id"`
	SenderID       uuid.UUID     `json:"sender_id"`
	Body           string        `json:"body"`
	Photos         []Photo       `json:"photos"`
	ListingID      *uuid.UUID    `json:"listing_id,omitempty"`
	ReplyTo        *uuid.UUID    `json:"reply_to,omitempty"`
	Deal           *DealMove     `json:"deal,omitempty"`
	Status         MessageStatus `json:"status"`
	Failure        string        `json:"failure,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
}

// RestoreMessage rebuilds a stored message. Stores only.
func RestoreMessage(st MessageState) Message { return Message{s: st} }

// State returns every stored field, with copies of the photos and the deal
// move. Stores only.
func (m *Message) State() MessageState {
	st := m.s
	st.Photos, st.Deal = m.Photos(), m.Deal()
	return st
}

// ConversationKind is a pair's one chat, or one support ticket's chat with
// ShopNexus, of which a person has any number.
type ConversationKind string

const (
	KindPair    ConversationKind = "pair"
	KindSupport ConversationKind = "support"
)

// Conversation is a chat of two accounts, stored with AccountA before
// AccountB: the one chat of two people, or a ticket's chat of a person
// with ShopNexus.
type Conversation struct {
	ID        uuid.UUID        `json:"id"`
	AccountA  uuid.UUID        `json:"account_a"`
	AccountB  uuid.UUID        `json:"account_b"`
	Kind      ConversationKind `json:"kind"`
	CreatedAt time.Time        `json:"created_at"`
}

// NewConversation starts the conversation of one and other, in either
// order.
func NewConversation(id, one, other uuid.UUID, now time.Time) Conversation {
	if bytes.Compare(one[:], other[:]) > 0 {
		one, other = other, one
	}
	return Conversation{ID: id, AccountA: one, AccountB: other, Kind: KindPair, CreatedAt: now}
}

// NewSupportConversation starts a ticket's chat between account and
// ShopNexus.
func NewSupportConversation(id, account uuid.UUID, now time.Time) Conversation {
	c := NewConversation(id, account, accountdomain.ShopNexusID, now)
	c.Kind = KindSupport
	return c
}

// Has reports whether account is a member.
func (c Conversation) Has(account uuid.UUID) bool {
	return account == c.AccountA || account == c.AccountB
}

// Other is the member who is not account.
func (c Conversation) Other(account uuid.UUID) uuid.UUID {
	if account == c.AccountA {
		return c.AccountB
	}
	return c.AccountA
}

// Member is one person's place in a conversation: when they archived it,
// nil while it is not archived, and the newest message they read, nil
// before the first.
type Member struct {
	ArchivedAt *time.Time
	LastRead   *uuid.UUID
}

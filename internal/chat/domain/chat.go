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
)

const (
	MaxText   = 1000
	MaxPhotos = 4
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

// Message is one message of a conversation. ListingID names the listing it
// is about, if any.
type Message struct {
	id             uuid.UUID
	conversationID uuid.UUID
	senderID       uuid.UUID
	body           string
	photos         []Photo
	listingID      *uuid.UUID
	status         MessageStatus
	failure        string
	createdAt      time.Time
}

// NewMessage checks a message as its sender wrote it: text 0 to 1,000
// characters after trimming and 0 to 4 photos, at least one of the two.
func NewMessage(id, conversation, sender uuid.UUID, body string, photos []uuid.UUID, listing *uuid.UUID, now time.Time) (Message, error) {
	e := ErrInvalidMessage
	body = strings.TrimSpace(body)
	n := utf8.RuneCountInString(body)
	if n > MaxText || (n == 0 && len(photos) == 0) {
		e = e.WithField("text", "1 to 1,000 characters, or photos")
	}
	sorted := slices.SortedFunc(slices.Values(photos), func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	if len(photos) > MaxPhotos || len(slices.Compact(sorted)) != len(photos) {
		e = e.WithField("photos", "up to 4 uploads, each once")
	}
	if len(e.Fields) > 0 {
		return Message{}, e
	}
	m := Message{
		id: id, conversationID: conversation, senderID: sender, body: body, listingID: listing,
		status: StatusSent, createdAt: now,
	}
	for _, p := range photos {
		m.photos = append(m.photos, Photo{UploadID: p})
	}
	if len(photos) > 0 {
		m.status = StatusProcessing
	}
	return m, nil
}

// Published records the processed photos; the message is sent.
func (m *Message) Published(keys map[uuid.UUID]Photo) {
	photos := make([]Photo, len(m.photos))
	for i, p := range m.photos {
		photos[i] = keys[p.UploadID]
		photos[i].UploadID = p.UploadID
	}
	m.photos, m.status = photos, StatusSent
}

// Failed records why media refused a photo.
func (m *Message) Failed(reason string) { m.status, m.failure = StatusFailed, reason }

// VisibleTo reports whether account may read the message: its sender
// always, the other member once it is sent.
func (m *Message) VisibleTo(account uuid.UUID) bool {
	return m.status == StatusSent || account == m.senderID
}

func (m *Message) ID() uuid.UUID             { return m.id }
func (m *Message) ConversationID() uuid.UUID { return m.conversationID }
func (m *Message) SenderID() uuid.UUID       { return m.senderID }
func (m *Message) Body() string              { return m.body }
func (m *Message) Photos() []Photo           { return append([]Photo{}, m.photos...) }
func (m *Message) ListingID() *uuid.UUID     { return m.listingID }
func (m *Message) Status() MessageStatus     { return m.status }
func (m *Message) Failure() string           { return m.failure }
func (m *Message) CreatedAt() time.Time      { return m.createdAt }

// UploadIDs returns the message's uploads in order.
func (m *Message) UploadIDs() []uuid.UUID {
	out := make([]uuid.UUID, len(m.photos))
	for i, p := range m.photos {
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
	Status         MessageStatus `json:"status"`
	Failure        string        `json:"failure,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
}

// RestoreMessage rebuilds a stored message. Stores only.
func RestoreMessage(st MessageState) Message {
	return Message{
		id: st.ID, conversationID: st.ConversationID, senderID: st.SenderID, body: st.Body, photos: st.Photos,
		listingID: st.ListingID, status: st.Status, failure: st.Failure, createdAt: st.CreatedAt,
	}
}

// State returns every stored field. Stores only.
func (m *Message) State() MessageState {
	return MessageState{
		ID: m.id, ConversationID: m.conversationID, SenderID: m.senderID, Body: m.body, Photos: m.Photos(),
		ListingID: m.listingID, Status: m.status, Failure: m.failure, CreatedAt: m.createdAt,
	}
}

// Conversation is the one conversation of two people, stored with
// AccountA before AccountB.
type Conversation struct {
	ID        uuid.UUID `json:"id"`
	AccountA  uuid.UUID `json:"account_a"`
	AccountB  uuid.UUID `json:"account_b"`
	CreatedAt time.Time `json:"created_at"`
}

// NewConversation starts the conversation of one and other, in either
// order.
func NewConversation(id, one, other uuid.UUID, now time.Time) Conversation {
	if bytes.Compare(one[:], other[:]) > 0 {
		one, other = other, one
	}
	return Conversation{ID: id, AccountA: one, AccountB: other, CreatedAt: now}
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

// Member is one person's place in a conversation: the message they read up
// to and when they archived it. The store moves the marker forward only.
type Member struct {
	ConversationID uuid.UUID
	AccountID      uuid.UUID
	LastRead       uuid.UUID
	ArchivedAt     *time.Time
}

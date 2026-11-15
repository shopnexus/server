// Package repository stores module chat's conversations, members and
// messages in Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/chat/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

var ErrNotFound = errors.New("chat: not found")

// Chats persists conversations and their messages, and notifies the open
// streams of the members a change concerns.
type Chats interface {
	// OpenPair inserts c with both members unless the pair has a
	// conversation, and returns the pair's conversation.
	OpenPair(ctx context.Context, c domain.Conversation) (domain.Conversation, error)
	// OpenSupport inserts c, a support conversation, and its two members,
	// unless a retried step inserted it already, and returns it.
	OpenSupport(ctx context.Context, c domain.Conversation) (domain.Conversation, error)
	// DropPair deletes pair conversation id and its members while no
	// message, deal or other row refers to it; one that has any stays, and
	// so does an unknown id.
	DropPair(ctx context.Context, id uuid.UUID) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Conversation, error)
	// FindPair returns the conversation of one and other, in either order,
	// or ErrNotFound.
	FindPair(ctx context.Context, one, other uuid.UUID) (domain.Conversation, error)
	// AddMessage inserts m and notifies the members m is visible to. A sent
	// m becomes the conversation's last message unless a newer one is, and
	// then takes the conversation out of both members' archives; it writes
	// message.sent unless it is a deal message. Adding the same ID again
	// does nothing.
	AddMessage(ctx context.Context, m domain.Message) error
	// DeleteMessage removes a message the send flow could not finish.
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	// GetMessage returns ErrNotFound for an unknown id.
	GetMessage(ctx context.Context, id uuid.UUID) (domain.Message, error)
	// SaveMessage writes a processed or failed message, moves the last
	// message when it is sent and newer, and notifies as AddMessage.
	SaveMessage(ctx context.Context, m domain.Message) error
	// Members returns the conversation's members by account, in one read.
	Members(ctx context.Context, conversation uuid.UUID) (map[uuid.UUID]domain.Member, error)
	// MarkRead moves account's marker to message when message is newer than
	// the marker and clears account's unread mark, in one update; a move
	// notifies other, a cleared mark notifies account.
	MarkRead(ctx context.Context, conversation, account, message, other uuid.UUID) error
	// MarkUnread sets account's unread mark; setting it notifies account.
	MarkUnread(ctx context.Context, conversation, account uuid.UUID) error
	// SetArchived archives the conversation for account at at, or
	// unarchives it for nil, touching nothing else.
	SetArchived(ctx context.Context, conversation, account uuid.UUID, at *time.Time) error
	// Messages lists the messages viewer may see with IDs below p.Before(),
	// newest first, at most p.Fetch().
	Messages(ctx context.Context, conversation, viewer uuid.UUID, p page.Request) ([]domain.Message, error)
	// Window lists conversation's sent messages with text whose IDs are at
	// most upTo, or every one for the zero upTo, newest first, at most n.
	Window(ctx context.Context, conversation, upTo uuid.UUID, n int) ([]domain.Message, error)
	// Inbox lists viewer's conversations with a sent message, archived or
	// not, without those whose other member viewer blocks, by last message
	// below p.Before(), newest first, at most p.Fetch().
	Inbox(ctx context.Context, viewer uuid.UUID, archived bool, p page.Request) ([]InboxRow, error)
	// Unread counts viewer's unread messages over unarchived conversations
	// the inbox lists, a conversation marked unread counting at least 1.
	Unread(ctx context.Context, viewer uuid.UUID) (int, error)
	// SupportUnread sums viewer's unread messages over its support chats,
	// which Unread leaves out.
	SupportUnread(ctx context.Context, viewer uuid.UUID) (int, error)
	// Threads reads the first and last sent messages and viewer's unread
	// count of each of viewer's conversations among ids; one without a
	// message is absent.
	Threads(ctx context.Context, viewer uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]ThreadRow, error)
	// Tokens splits q with the search index's tokenizer: lowercase, accents
	// folded, split on anything but letters and digits.
	Tokens(ctx context.Context, q string) ([]string, error)
	// SearchThreads lists viewer's conversations with a sent message,
	// archived or not, without those whose other member viewer blocks,
	// where t matches the other member's name or the title of a listing a
	// message names; newest last message first, at most limit.
	SearchThreads(ctx context.Context, viewer uuid.UUID, t Terms, limit int) ([]InboxRow, error)
	// ReplyThreads reads seller's pair conversations whose first sent
	// message that is not a deal message came at or after since from
	// another person and names one of seller's listings, or names none in a
	// conversation holding a deal where seller sells: that message's time
	// and seller's first such message's, if any.
	ReplyThreads(ctx context.Context, seller uuid.UUID, since time.Time) ([]domain.ReplyThread, error)
	// SearchMessages lists the sent messages matching t in the
	// conversations SearchThreads would consider, IDs below p.Before(),
	// newest first, at most p.Fetch().
	SearchMessages(ctx context.Context, viewer uuid.UUID, t Terms, p page.Request) ([]MessageHitRow, error)
}

// InboxRow is a conversation in viewer's inbox.
type InboxRow struct {
	Conversation domain.Conversation
	Other        uuid.UUID
	Last         domain.Message
	Unread       int
	// Archived is whether viewer archived it.
	Archived bool
}

// ThreadRow is a conversation's last message and a viewer's unread count.
type ThreadRow struct {
	First  domain.Message
	Last   domain.Message
	Unread int
}

// Terms is a search's tokens: each of Whole matches a whole word, Prefix
// the start of one.
type Terms struct {
	Whole  []string
	Prefix string
}

// MessageHitRow is a message a search found and the other member of its
// conversation.
type MessageHitRow struct {
	Message domain.Message
	Other   uuid.UUID
}

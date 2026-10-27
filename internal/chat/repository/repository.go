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
//
//nolint:interfacebloat // One store for one module's three tables, read and written together.
type Chats interface {
	// OpenPair inserts c with both members unless the pair has a
	// conversation, and returns the pair's conversation.
	OpenPair(ctx context.Context, c domain.Conversation) (domain.Conversation, error)
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Conversation, error)
	// FindPair returns the conversation of one and other, in either order,
	// or ErrNotFound.
	FindPair(ctx context.Context, one, other uuid.UUID) (domain.Conversation, error)
	// AddMessage inserts m and notifies the members m is visible to. A sent
	// m becomes the conversation's last message and takes the conversation
	// out of both members' archives. Adding the same ID again does nothing.
	AddMessage(ctx context.Context, m domain.Message) error
	// DeleteMessage removes a message the send flow could not finish.
	DeleteMessage(ctx context.Context, id uuid.UUID) error
	// GetMessage returns ErrNotFound for an unknown id.
	GetMessage(ctx context.Context, id uuid.UUID) (domain.Message, error)
	// SaveMessage writes a processed or failed message, moves the last
	// message when it is sent and newer, and notifies as AddMessage.
	SaveMessage(ctx context.Context, m domain.Message) error
	// Member returns ErrNotFound for an account outside the conversation.
	Member(ctx context.Context, conversation, account uuid.UUID) (domain.Member, error)
	// MarkRead moves account's marker to message when message is newer than
	// the marker, in one conditional update, and reports whether it moved;
	// a move notifies other.
	MarkRead(ctx context.Context, conversation, account, message, other uuid.UUID) (bool, error)
	// SetArchived archives the conversation for account at at, or
	// unarchives it for nil, touching nothing else.
	SetArchived(ctx context.Context, conversation, account uuid.UUID, at *time.Time) error
	// Messages lists the messages viewer may see with IDs below p.Before(),
	// newest first, at most p.Fetch().
	Messages(ctx context.Context, conversation, viewer uuid.UUID, p page.Request) ([]domain.Message, error)
	// Inbox lists viewer's conversations with a sent message, archived or
	// not, without those whose other member viewer blocks, by last message
	// below p.Before(), newest first, at most p.Fetch().
	Inbox(ctx context.Context, viewer uuid.UUID, archived bool, p page.Request) ([]InboxRow, error)
	// Unread counts viewer's unread messages over unarchived conversations
	// the inbox lists.
	Unread(ctx context.Context, viewer uuid.UUID) (int, error)
}

// InboxRow is a conversation in viewer's inbox.
type InboxRow struct {
	Conversation domain.Conversation
	Other        uuid.UUID
	Last         domain.Message
	Unread       int
}

// Package repository stores module support's tickets, their events and
// who wrote each ShopNexus reply.
package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/support/domain"
)

// Ticket is one ticket as stored, with its events oldest first.
type Ticket struct {
	ID             uuid.UUID
	AccountID      uuid.UUID
	ConversationID uuid.UUID
	Kind           domain.Kind
	Status         domain.Status
	OrderID        *uuid.UUID
	CarrierName    string
	FirstMessageID *uuid.UUID
	LastMessageAt  time.Time
	LastFrom       domain.From
	OpenedAt       time.Time
	ResolvedAt     *time.Time
	ResolvedBy     *uuid.UUID
	Events         []Event
}

// Event is a ticket opening, resolving or reopening; MemberID is the
// member who did it, nil for the person or the system.
type Event struct {
	Kind     string
	MemberID *uuid.UUID
	At       time.Time
}

// Reply is who wrote a ShopNexus message: a member's name, or Automatic
// for one ShopNexus sent by itself.
type Reply struct {
	Member    string
	Automatic bool
}

// Tickets stores tickets.
type Tickets interface {
	// Insert stores t, open and waiting for an answer, with its opened
	// event, unless the account opened domain.DailyLimit tickets after since
	// (domain.ErrTooMany). A key the account used already, or for
	// not_received an order with a ticket, returns that ticket and true
	// instead; t's own row, from a retried call, returns it and false. The
	// account is locked for the transaction.
	Insert(ctx context.Context, t Ticket, key uuid.UUID, carrierKey string, since time.Time) (Ticket, bool, error)
	// OpenedSince counts the tickets account opened after since, without
	// the lock: the check before Insert, which counts again under it.
	OpenedSince(ctx context.Context, account uuid.UUID, since time.Time) (int, error)
	// SetFirstMessage records the message the ticket's card is.
	SetFirstMessage(ctx context.Context, id, message uuid.UUID) error
	// Get reads a ticket; domain.ErrNotFound for none.
	Get(ctx context.Context, id uuid.UUID) (Ticket, error)
	// ByConversation reads the ticket of a support chat; domain.ErrNotFound
	// for a chat with none.
	ByConversation(ctx context.Context, conversation uuid.UUID) (Ticket, error)
	// ByOrder reads account's not_received ticket of order.
	ByOrder(ctx context.Context, account, order uuid.UUID) (Ticket, error)
	// OfAccount reads account's tickets, open first, each newest activity
	// first.
	OfAccount(ctx context.Context, account uuid.UUID) ([]Ticket, error)
	// Queue is a query: tickets of status, of kind when kind is set, newest
	// activity first, after the ticket p.Cursor names, at most p.Fetch().
	Queue(ctx context.Context, status domain.Status, kind *domain.Kind, p page.Request) ([]Ticket, error)
	CountOpen(ctx context.Context) (int, error)
	CountOpenOf(ctx context.Context, account uuid.UUID) (int, error)
	// SameCarrier counts carrier requests for carrierKey opened after since.
	SameCarrier(ctx context.Context, carrierKey string, since time.Time) (int, error)
	// Messaged records a message in conversation's ticket at at. The
	// person's message reopens a resolved ticket, with a reopened event, and
	// reports true. A chat with no ticket changes nothing.
	Messaged(ctx context.Context, conversation uuid.UUID, from domain.From, at time.Time) (bool, error)
	// Resolve and Reopen set the ticket's status with its event and, when
	// actor is a member, the audit row. A ticket in that status already is
	// domain.ErrNotAllowed.
	Resolve(ctx context.Context, id uuid.UUID, actor *audit.Actor, at time.Time) (Ticket, error)
	Reopen(ctx context.Context, id uuid.UUID, actor *audit.Actor, at time.Time) (Ticket, error)
	// RecordReply records who wrote message, a ShopNexus message of the
	// ticket: actor's member with its ticket.reply audit row, or nobody for
	// an automatic one (nil actor).
	RecordReply(ctx context.Context, ticket, message uuid.UUID, actor *audit.Actor, at time.Time) error
	// Members reads who wrote each of messages; a message with no reply row
	// is absent.
	Members(ctx context.Context, messages []uuid.UUID) (map[uuid.UUID]Reply, error)
	// ResolveAllOf resolves an account's open tickets, by nobody.
	ResolveAllOf(ctx context.Context, account uuid.UUID, at time.Time) error
}

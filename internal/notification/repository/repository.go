// Package repository stores module notification's notices, push tokens and
// settings in Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/page"
)

// ErrNotFound is a notice the account does not have.
var ErrNotFound = errors.New("notification not found")

// Notices persists notices and push tokens.
type Notices interface {
	// Add writes n for event and returns its ID. It collapses on the
	// recipient's unread notice with the same key, writes nothing for an
	// event already written for the recipient, deletes the recipient's
	// notices last bumped before expired, and notifies the stream, in one
	// transaction. When the recipient has a newer notice with the key, read
	// or unread, Add writes nothing and returns that notice's ID and late.
	// A new notice takes fresh, minted by the caller so a retry reuses it.
	Add(ctx context.Context, event, fresh uuid.UUID, n domain.Notice, expired time.Time) (id uuid.UUID, late bool, err error)
	// List is a query: the account's notices, newest bumped first, below
	// p's cursor, at most p.Fetch().
	List(ctx context.Context, account uuid.UUID, p page.Request) ([]Row, error)
	Unread(ctx context.Context, account uuid.UUID) (int, error)
	// Read marks one of the account's notices read; ErrNotFound otherwise.
	Read(ctx context.Context, account, id uuid.UUID, at time.Time) error
	// ReadAll marks the account's unread notices read and returns their IDs,
	// an empty slice when there were none.
	ReadAll(ctx context.Context, account uuid.UUID, at time.Time) ([]uuid.UUID, error)
	// MarkUnread makes the account's read notices among ids unread: per
	// collapse key only the newest, and none for a key with an unread
	// notice. Other IDs are skipped.
	MarkUnread(ctx context.Context, account uuid.UUID, ids []uuid.UUID) error
	// PutToken stores the push token of a session, replacing any, and takes
	// the token from every other session. A deleted account is
	// accountdomain.ErrSessionRevoked.
	PutToken(ctx context.Context, session, account uuid.UUID, token string, at time.Time) error
	// DeleteToken forgets a session's token; an absent one is fine.
	DeleteToken(ctx context.Context, session uuid.UUID) error
	// Forget deletes the account's notices, push tokens and settings.
	Forget(ctx context.Context, account uuid.UUID) error
	Tokens(ctx context.Context, account uuid.UUID) ([]Token, error)
}

// Settings keeps what each account says about its pushes.
type Settings interface {
	// Get is the account's settings, domain.DefaultSettings when it never
	// saved any; it writes nothing.
	Get(ctx context.Context, account uuid.UUID) (domain.Settings, error)
	// Put replaces the account's settings. A deleted account is
	// accountdomain.ErrSessionRevoked.
	Put(ctx context.Context, account uuid.UUID, s domain.Settings) error
}

// Row is a notice as the list reads it.
type Row struct {
	ID       uuid.UUID
	Type     domain.Type
	Params   domain.Params
	Read     bool
	BumpedAt time.Time
}

// Token is a session's push token. The worker journals it.
type Token struct {
	Session uuid.UUID `json:"session"`
	Token   string    `json:"token"`
}

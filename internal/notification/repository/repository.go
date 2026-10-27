// Package repository stores module notification's notices and push tokens
// in Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/platform/page"
)

// ErrNotFound is a notice the account does not have.
var ErrNotFound = errors.New("notification not found")

// Notices persists notices and push tokens.
type Notices interface {
	// Add writes n for event and returns its ID. It collapses on the
	// recipient's unread notice with the same key unless that one is newer,
	// writes nothing for an event already written for the recipient,
	// deletes the recipient's notices from before expired, and notifies the
	// stream, in one transaction.
	Add(ctx context.Context, event uuid.UUID, n domain.Notice, expired time.Time) (uuid.UUID, error)
	// List is a query: the account's notices, newest bumped first, below
	// p's cursor, at most p.Fetch().
	List(ctx context.Context, account uuid.UUID, p page.Request) ([]Row, error)
	Unread(ctx context.Context, account uuid.UUID) (int, error)
	// Read marks one of the account's notices read; ErrNotFound otherwise.
	Read(ctx context.Context, account, id uuid.UUID, at time.Time) error
	ReadAll(ctx context.Context, account uuid.UUID, at time.Time) error
	// PutToken stores the push token of a session, replacing any.
	PutToken(ctx context.Context, session, account uuid.UUID, token string, at time.Time) error
	// DeleteToken forgets a session's token; an absent one is fine.
	DeleteToken(ctx context.Context, session uuid.UUID) error
	Tokens(ctx context.Context, account uuid.UUID) ([]Token, error)
}

// Row is a notice as the list reads it.
type Row struct {
	ID       uuid.UUID
	Type     domain.Type
	Params   map[string]any
	Read     bool
	BumpedAt time.Time
}

// Token is a session's push token.
type Token struct {
	Session uuid.UUID
	Token   string
}

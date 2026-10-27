// Package service holds module notification's use cases: a person reads
// their notices, marks them read, and gives the push token of their
// session.
package service

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/notification/domain"
	"github.com/shopnexus/server2/internal/notification/repository"
	"github.com/shopnexus/server2/internal/platform/authn"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Service is module notification's contract.
type Service interface {
	// List reads the account's notices, newest first, and its unread count.
	List(ctx context.Context, account uuid.UUID, p page.Request) (List, error)
	Unread(ctx context.Context, account uuid.UUID) (int, error)
	// Read marks one of the account's notices read; domain.ErrNotFound
	// otherwise.
	Read(ctx context.Context, account, id uuid.UUID) error
	ReadAll(ctx context.Context, account uuid.UUID) error
	// PutToken stores the push token of the caller's session.
	PutToken(ctx context.Context, p authn.Principal, token string) error
	// DeleteToken forgets it.
	DeleteToken(ctx context.Context, p authn.Principal) error

	// Record writes a notice the worker decided, for event; see
	// repository.Notices.Add.
	Record(ctx context.Context, event uuid.UUID, n domain.Notice) (Recorded, error)
	// Tokens lists the account's push tokens by session.
	Tokens(ctx context.Context, account uuid.UUID) ([]Token, error)
	// ForgetToken deletes a session's token, ended or dead.
	ForgetToken(ctx context.Context, session uuid.UUID) error
}

// Recorded is the notice Record wrote. Late means a newer notice with the
// same key was there already: nothing was written, ID is the newer one's,
// and there is nothing to push.
type Recorded struct {
	ID   uuid.UUID `json:"id"`
	Late bool      `json:"late"`
}

// Token is a session's push token.
type Token struct {
	Session uuid.UUID `json:"session"`
	Token   string    `json:"token"`
}

// Item is a notice as the list shows it.
type Item struct {
	ID     uuid.UUID     `json:"id"`
	Type   domain.Type   `json:"type"`
	Text   string        `json:"text"`
	Target domain.Target `json:"target"`
	Unread bool          `json:"unread"`
	At     time.Time     `json:"at"`
}

// List is a page of notices and the unread count.
type List struct {
	Items      []Item     `json:"items"`
	NextCursor *uuid.UUID `json:"next_cursor,omitempty"`
	Unread     int        `json:"unread"`
}

// Deps is what New builds Notification from.
type Deps struct {
	Notices repository.Notices
	Now     func() time.Time
}

// Notification implements Service in this process.
type Notification struct {
	notices repository.Notices
	now     func() time.Time
}

var _ Service = (*Notification)(nil)

func New(d Deps) *Notification { return &Notification{notices: d.Notices, now: d.Now} }

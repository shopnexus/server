// Package service holds module notification's use cases: a person reads
// their notices, marks them read, gives the push token of their session,
// and says which notices push.
package service

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/locale"

	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/notification/repository"
	"github.com/shopnexus/server/internal/platform/page"
)

// Service is module notification's contract.
type Service interface {
	// List reads the account's notices, newest first, written in language
	// tag, and its unread count.
	//
	// UC-SHR-03
	List(ctx context.Context, account uuid.UUID, p page.Request, tag locale.Tag) (List, error)
	// Unread counts the account's unread notices.
	//
	// UC-SHR-03
	Unread(ctx context.Context, account uuid.UUID) (int, error)
	// Read marks one of the account's notices read; domain.ErrNotFound
	// otherwise.
	//
	// UC-SHR-03 E1
	Read(ctx context.Context, account, id uuid.UUID) error
	// ReadAll marks every unread notice of the account's read and returns
	// their IDs, for an undo through MarkUnread.
	//
	// UC-SHR-03 A13
	ReadAll(ctx context.Context, account uuid.UUID) ([]uuid.UUID, error)
	// MarkUnread makes the account's notices among ids unread; see
	// repository.Notices.MarkUnread.
	//
	// UC-SHR-03 A14
	MarkUnread(ctx context.Context, account uuid.UUID, ids []uuid.UUID) error
	// PutToken stores the push token of the account's session.
	//
	// UC-SHR-03 A7, E2, E4
	PutToken(ctx context.Context, session, account uuid.UUID, token string) error
	// DeleteToken deletes a session's token; the handler calls it for the
	// caller's session, the worker for an ended session or a dead token.
	//
	// UC-SHR-03 A7, A10
	DeleteToken(ctx context.Context, session uuid.UUID) error
	// Settings are the account's push settings, the defaults when it
	// never saved any.
	//
	// UC-SHR-03 A8
	Settings(ctx context.Context, account uuid.UUID) (domain.Settings, error)
	// PutSettings replaces the account's push settings;
	// domain.ErrInvalidQuietHours for quiet hours that start when they end.
	//
	// UC-SHR-03 A8, E3, E4 · BR-SHR-01
	PutSettings(ctx context.Context, account uuid.UUID, s domain.Settings) error
	// Forget deletes a deleted account's notices, push tokens and
	// settings; the worker calls it on account.deleted.
	Forget(ctx context.Context, account uuid.UUID) error

	// Record writes a notice the worker decided, for event, with fresh as
	// a new notice's ID; see repository.Notices.Add.
	//
	// UC-SHR-03 A1, A6 · BR-SHR-02 · NFR-IDEM-01
	Record(ctx context.Context, event, fresh uuid.UUID, n domain.Notice) (Recorded, error)
	// Tokens lists the account's push tokens by session.
	//
	// UC-SHR-03
	Tokens(ctx context.Context, account uuid.UUID) ([]Token, error)
}

// Recorded is the notice Record wrote. Late means a newer notice with the
// same key was there already: nothing was written, ID is the newer one's,
// and there is nothing to push.
type Recorded struct {
	ID   uuid.UUID `json:"id"`
	Late bool      `json:"late"`
}

// Token is a session's push token.
type Token = repository.Token

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
	Notices  repository.Notices
	Settings repository.Settings
	Now      func() time.Time
}

// Notification implements Service in this process.
type Notification struct{ d Deps }

var _ Service = (*Notification)(nil)

func New(d Deps) *Notification {
	return &Notification{d: d}
}

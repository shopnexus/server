package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Accounts persists accounts and their blocks.
type Accounts interface {
	// Create inserts a without credentials; inserting the same id again does
	// nothing. Sign-up goes through Credentials.SignUp instead.
	Create(ctx context.Context, a domain.Account) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Account, error)
	// GetMany returns the accounts among ids in any order; an unknown ID is
	// left out.
	GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Account, error)
	// Save writes a when its row still has a.WriteID() or already has
	// writeID, then sets a's write ID to writeID. Otherwise ErrConflict. It
	// writes entry, when not nil, and evs in the same transaction; a
	// conflict writes neither. A durable step that retries a save passes the
	// event IDs it took from the journal with the writeID, and the retry
	// writes no second event.
	Save(ctx context.Context, a *domain.Account, writeID uuid.UUID, entry *audit.Entry, evs ...events.Event) error
	// List is a query: accounts with IDs below p.Before(), newest first, at
	// most p.Fetch().
	List(ctx context.Context, p page.Request) ([]domain.Account, error)
	// Block records that blocker blocks blocked, as id, and deletes the
	// follows between the two, both ways; blocking again changes nothing.
	Block(ctx context.Context, id, blocker, blocked uuid.UUID, at time.Time) error
	// Unblock lifts the block; an absent one is fine.
	Unblock(ctx context.Context, blocker, blocked uuid.UUID) error
	// TouchActive sets last_active_at to at unless it is less than 30
	// seconds older, so devices of one account do not pile up writes. It
	// leaves write_id alone: it is no change of the aggregate's.
	TouchActive(ctx context.Context, id uuid.UUID, at time.Time) error
	// Blocked reports a block between a and b, in either direction.
	Blocked(ctx context.Context, a, b uuid.UUID) (bool, error)
	// ListBlocks is a query: blocker's blocks with IDs below p.Before(),
	// newest first, at most p.Fetch().
	ListBlocks(ctx context.Context, blocker uuid.UUID, p page.Request) ([]Block, error)
}

// Block is one account a blocker blocks.
type Block struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	At        time.Time
}

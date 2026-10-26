// Package repository stores module account's accounts in Postgres.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/account/domain"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/events"
	"github.com/shopnexus/server2/internal/platform/page"
)

var (
	ErrNotFound = errors.New("account: not found")
	ErrConflict = errors.New("account: changed meanwhile")
)

// Accounts persists accounts. Each method is its own transaction.
type Accounts interface {
	// Create inserts a; inserting the same id again does nothing, so a retried
	// step is safe.
	Create(ctx context.Context, a domain.Account) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Account, error)
	// Save writes a when its row still has a.WriteID() or already has
	// writeID, then sets a's write ID to writeID. Otherwise ErrConflict. It
	// writes evs in the same transaction; a conflict writes none. A durable
	// step that retries a save passes the event IDs it took from the
	// journal with the writeID, and the retry writes no second event.
	Save(ctx context.Context, a *domain.Account, writeID uuid.UUID, evs ...events.Event) error
	// SaveAudited is Save that also writes e in the same transaction.
	SaveAudited(ctx context.Context, a *domain.Account, writeID uuid.UUID, e audit.Entry, evs ...events.Event) error
	// List is a query: accounts with IDs below p.Before(), newest first, at
	// most p.Fetch().
	List(ctx context.Context, p page.Request) ([]domain.Account, error)
	// Delete removes the account; deleting a missing one does nothing. Only
	// the sign-up compensation calls it.
	Delete(ctx context.Context, id uuid.UUID) error
	// Block records that blocker blocks blocked, as id; blocking again
	// changes nothing.
	Block(ctx context.Context, id, blocker, blocked uuid.UUID, at time.Time) error
	// Unblock lifts the block; an absent one is fine.
	Unblock(ctx context.Context, blocker, blocked uuid.UUID) error
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

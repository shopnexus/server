// Package repository stores module assist's video reads in Postgres. Each
// method is its own transaction.
package repository

import (
	"context"
	"errors"
	"uuid"

	"github.com/shopnexus/server2/internal/assist/domain"
)

// ErrConflict is a save that lost a race: the row changed meanwhile.
var ErrConflict = errors.New("assist: changed meanwhile")

// VideoReads persists video reads.
type VideoReads interface {
	// Insert stores a new read and notifies its account. It takes a share
	// lock on the account's row, so a deleted account gets
	// accountdomain.ErrSessionRevoked, and counts the account's reads of the
	// last domain.ReadWindow before r's start one insert at a time:
	// domain.ReadLimit of them is domain.ErrReadLimit. A read already
	// stored passes without a count or a notice, so a retried step
	// succeeds.
	Insert(ctx context.Context, r domain.VideoRead) error
	// Save writes r as writeID when the row still has r's write ID, notifies
	// the account and records writeID on r. A row already at writeID passes
	// without a notice; any other row is ErrConflict.
	Save(ctx context.Context, r *domain.VideoRead, writeID uuid.UUID) error
	// Get returns domain.ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.VideoRead, error)
	// Delete removes a read; an absent one is fine.
	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteAccount removes every read of the account.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}

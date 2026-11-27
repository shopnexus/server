// Package repository stores module payment's payments.
package repository

import (
	"context"
	"errors"
	"uuid"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/platform/events"
)

// ErrConflict is a save that lost to another write; the caller reads again.
var ErrConflict = errors.New("payment: changed meanwhile")

// Store stores payments.
type Store interface {
	// Insert stores p unless a payment for the same product row exists, and
	// returns the stored one.
	Insert(ctx context.Context, p domain.Payment) (domain.Payment, error)
	// Get reads a payment; domain.ErrNotFound for none.
	Get(ctx context.Context, id uuid.UUID) (domain.Payment, error)
	// GetMany reads the payments among ids, keyed by ID.
	GetMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Payment, error)
	// Save writes p under writeID with evs when its write ID is still the
	// stored one; ErrConflict otherwise, and nothing is written.
	Save(ctx context.Context, p *domain.Payment, writeID uuid.UUID, evs ...events.Event) error
}

// Package repository stores module media's uploads in Postgres. Each method
// is its own transaction.
package repository

import (
	"context"
	"errors"
	"uuid"

	"github.com/shopnexus/server2/internal/media/domain"
)

var (
	ErrNotFound = errors.New("media: not found")
	ErrConflict = errors.New("media: changed meanwhile")
)

// Uploads persists uploads.
type Uploads interface {
	// Create inserts u; the same id again does nothing.
	Create(ctx context.Context, u domain.Upload) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Upload, error)
	// GetMany returns the uploads of ids it finds, in no order.
	GetMany(ctx context.Context, ids []uuid.UUID) ([]domain.Upload, error)
	// Save writes u when its row still has u.WriteID() or already has
	// writeID, then sets its write ID. Otherwise ErrConflict.
	Save(ctx context.Context, u *domain.Upload, writeID uuid.UUID) error
	// SaveAll is Save for every upload of ups in one transaction: all are
	// written or none, and ErrConflict when any row moved.
	SaveAll(ctx context.Context, ups []*domain.Upload, writeID uuid.UUID) error
}

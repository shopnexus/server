// Package repository stores module trust's reviews in Postgres. Each method
// is its own statement.
package repository

import (
	"context"
	"errors"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
	"github.com/shopnexus/server2/internal/trust/domain"
)

var (
	ErrNotFound = errors.New("trust: not found")
	ErrConflict = errors.New("trust: changed meanwhile")
)

// Reviews persists reviews, one per order.
type Reviews interface {
	// ByOrder returns the review of an order, deleted or not, or
	// ErrNotFound.
	ByOrder(ctx context.Context, order uuid.UUID) (domain.Review, error)
	// Insert adds the order's first review; an order that has one is
	// ErrConflict.
	Insert(ctx context.Context, r domain.Review) error
	// Save writes r when its row still has r.WriteID(), then sets its
	// write ID to writeID; otherwise ErrConflict and nothing is written.
	Save(ctx context.Context, r *domain.Review, writeID uuid.UUID) error
	// OfSeller is a query: seller's reviews not deleted with IDs below
	// p.Before(), newest first, at most p.Fetch().
	OfSeller(ctx context.Context, seller uuid.UUID, p page.Request) ([]domain.Review, error)
	// Summary is the seller's average stars and number of reviews not
	// deleted; no reviews give 0 and 0.
	Summary(ctx context.Context, seller uuid.UUID) (average float64, count int, err error)
}

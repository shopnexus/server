// Package repository stores module trust's reviews, reports and scam
// warnings in Postgres. Each method is one statement, except the review's
// Insert and the scam warnings' Record and Save, which write in one
// transaction.
package repository

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
)

var (
	ErrNotFound = errors.New("trust: not found")
	ErrConflict = errors.New("trust: changed meanwhile")
)

// Reviews persists reviews, one per order and role.
type Reviews interface {
	// ByOrder returns the review of an order in role, deleted or not, or
	// ErrNotFound.
	ByOrder(ctx context.Context, order uuid.UUID, role domain.Role) (domain.Review, error)
	// OfOrders returns every review of the orders given, both roles,
	// deleted or not, in any order.
	OfOrders(ctx context.Context, orders []uuid.UUID) ([]domain.Review, error)
	// Get returns a review by its ID, or ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (domain.Review, error)
	// Insert adds the order's first review in r's role, makes the other
	// role's review visible from r's creation, and writes review.posted.
	// An order with a review in the role is ErrConflict, and so is a
	// hidden r when the other role's review exists.
	Insert(ctx context.Context, r domain.Review) error
	// Save writes r when its row still has r.WriteID(), then sets its
	// write ID to writeID; otherwise ErrConflict and nothing is written.
	// A first reply also writes review.replied with writeID as its ID.
	Save(ctx context.Context, r *domain.Review, writeID uuid.UUID) error
	// OfSubject is a query: subject's reviews in role, visible at now and
	// not deleted, with IDs below p.Before(), newest first, at most
	// p.Fetch().
	OfSubject(ctx context.Context, subject uuid.UUID, role domain.Role, now time.Time, p page.Request) ([]domain.Review, error)
	// Summary is subject's average stars and number of reviews in role,
	// visible at now and not deleted; no reviews give 0 and 0.
	Summary(ctx context.Context, subject uuid.UUID, role domain.Role, now time.Time) (average float64, count int, err error)
	// TagCounts counts each tag over the reviews Summary counts, most
	// used first, then by tag; a tag no review carries is left out.
	TagCounts(ctx context.Context, subject uuid.UUID, role domain.Role, now time.Time) ([]TagCount, error)
	// MarkHelpful records that account found the review helpful; a second
	// mark changes nothing.
	MarkHelpful(ctx context.Context, review, account uuid.UUID, at time.Time) error
	// UnmarkHelpful takes account's mark back; an absent one is fine.
	UnmarkHelpful(ctx context.Context, review, account uuid.UUID) error
	// Helpful counts the helpful marks of the reviews given and tells
	// whether viewer made each, in one query; a review without marks is
	// left out. A nil viewer made none.
	Helpful(ctx context.Context, reviews []uuid.UUID, viewer *uuid.UUID) (map[uuid.UUID]HelpfulMarks, error)
}

// TagCount is how many reviews carry one tag.
type TagCount struct {
	Tag   domain.Tag
	Count int
}

// HelpfulMarks are the helpful marks of one review.
type HelpfulMarks struct {
	Count int
	// Viewer is whether the viewer made one of them.
	Viewer bool
}

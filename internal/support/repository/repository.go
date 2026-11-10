// Package repository stores module support's feedback.
package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/page"
)

// Row is one feedback.
type Row struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	Body      string
	CreatedAt time.Time
}

// Feedback stores feedback.
type Feedback interface {
	// Add inserts r under the request's key, unless the account sent
	// domain.DailyLimit feedbacks after since (domain.ErrFeedbackLimit). A
	// key the account used already returns that request's row first, at
	// the limit too. The account is locked for the transaction.
	Add(ctx context.Context, r Row, key uuid.UUID, since time.Time) (Row, error)
	// List is a query: feedback with IDs below p.Before(), newest first, at
	// most p.Fetch().
	List(ctx context.Context, p page.Request) ([]Row, error)
}

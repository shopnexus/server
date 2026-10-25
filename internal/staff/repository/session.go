package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/staff/domain"
)

// Sessions keeps staff sessions. Each method is one statement.
type Sessions interface {
	Create(ctx context.Context, s domain.Session) error
	// Live is a query joining the session whose token hashes to tokenHash
	// with its member. It returns ErrNotFound unless the session lives at
	// now.
	Live(ctx context.Context, tokenHash []byte, now time.Time) (domain.Grant, error)
	// Revoke signs one session out; a second call keeps the first time.
	Revoke(ctx context.Context, id uuid.UUID, now time.Time) error
}

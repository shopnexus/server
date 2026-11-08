package repository

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/console/domain"
	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Members persists console members. Each method is its own transaction.
type Members interface {
	// Create inserts m and writes entry. Inserting the same id again does
	// nothing; another member's email is ErrEmailTaken.
	Create(ctx context.Context, m domain.Member, entry audit.Entry) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (domain.Member, error)
	// FindByEmail takes an email in contact.NormalizeEmail form and returns
	// ErrNotFound when no member has it.
	FindByEmail(ctx context.Context, email string) (domain.Member, error)
	// List is a query: members with IDs below p.Before(), newest first, at
	// most p.Fetch().
	List(ctx context.Context, p page.Request) ([]domain.Member, error)
	// Save writes m under the write_id rule of ARCHITECTURE.md and sets m's
	// write ID; a lost race is ErrConflict. It writes entry, when not nil,
	// in the same transaction; a conflict writes none.
	Save(ctx context.Context, m *domain.Member, writeID uuid.UUID, entry *audit.Entry) error
}

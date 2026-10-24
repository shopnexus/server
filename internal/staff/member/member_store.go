package member

import (
	"context"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// Store persists members. Each method is its own transaction.
type Store interface {
	// CreateAudited inserts m and writes e. Inserting the same id again does
	// nothing; another member's email is ErrEmailTaken.
	CreateAudited(ctx context.Context, m Member, e audit.Entry) error
	// Get returns ErrNotFound for an unknown id.
	Get(ctx context.Context, id uuid.UUID) (Member, error)
	// FindByEmail takes an email in contact.NormalizeEmail form and returns
	// ErrNotFound when no member has it.
	FindByEmail(ctx context.Context, email string) (Member, error)
	// List is a query: members with IDs below p.Before(), newest first, at
	// most p.Fetch().
	List(ctx context.Context, p page.Request) ([]Member, error)
	// Save writes m under the write_id rule of ARCHITECTURE.md and sets m's
	// write ID; a lost race is ErrConflict.
	Save(ctx context.Context, m *Member, writeID uuid.UUID) error
	// SaveAudited is Save that writes e in the same transaction.
	SaveAudited(ctx context.Context, m *Member, writeID uuid.UUID, e audit.Entry) error
}

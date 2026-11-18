package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/trust/domain"
)

// Reports persists reports. A reporter has at most one open report of a
// listing and one of an account; a resolved report is no longer open.
type Reports interface {
	// Open adds r or, when its reporter has an open report of the same
	// target that no case resolved, replaces that report's reason, note and conversation with r's;
	// it returns the stored report and whether it was added.
	Open(ctx context.Context, r domain.Report) (domain.Report, bool, error)
	// Get returns a report, open or withdrawn, or ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (domain.Report, error)
	// Withdraw marks an open report withdrawn at at; a withdrawn one keeps
	// its time.
	Withdraw(ctx context.Context, id uuid.UUID, at time.Time) error
}

package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/trust/domain"
)

// Case is a report case as stored: the reports about one account, not
// withdrawn, that share one resolution, or none for the open case.
type Case struct {
	AccountID uuid.UUID
	// Key is what a list of cases pages by: the latest report for an open
	// case, the resolution for a resolved one.
	Key      uuid.UUID
	Reports  int
	Reasons  map[domain.Reason]int
	Listings []domain.ListingTally
	FirstAt  time.Time
	LastAt   time.Time
	// Resolution is nil for the open case.
	Resolution *Resolution
}

// Resolution is how a console member closed a case. MemberName is the
// member's display name, read with the resolution.
type Resolution struct {
	ID         uuid.UUID
	Outcome    domain.Outcome
	MemberID   uuid.UUID
	MemberName string
	At         time.Time
}

// Cases groups reports into cases and resolves them. A resolve or a reopen
// takes a lock on the account, so the two never interleave for one account.
type Cases interface {
	// Open lists the open cases whose latest report comes before the one
	// p.Before() names, latest report first, at most p.Fetch().
	Open(ctx context.Context, p page.Request) ([]Case, error)
	// CountOpen counts the accounts with an open case.
	CountOpen(ctx context.Context) (int, error)
	// CountByStatus is a query: the open cases with a report made at or
	// after since, and the resolved cases, as Resolved lists them, resolved
	// at or after since; a status without one is left out.
	CountByStatus(ctx context.Context, since time.Time) (map[domain.CaseStatus]int, error)
	// Resolved lists the resolved cases, not reopened, whose resolution
	// comes before the one p.Before() names, latest resolution first, at
	// most p.Fetch(). A resolution whose reports were all withdrawn since is
	// left out.
	Resolved(ctx context.Context, p page.Request) ([]Case, error)
	// Current returns account's open case or, without one, its latest
	// resolved case, with the case's reports newest first; ErrNotFound
	// when the account has neither.
	Current(ctx context.Context, account uuid.UUID) (Case, []domain.Report, error)
	// Resolve stamps account's open reports with r and writes entry in the
	// same transaction. Without an open report nothing is written: nil when
	// the resolution Current shows has r's outcome, else ErrCaseNotOpen.
	Resolve(ctx context.Context, account uuid.UUID, r Resolution, entry audit.Entry) error
	// Reopen marks account's latest resolution reopened, at the time given,
	// and opens its reports again, except those whose reporter has a newer
	// open report of the same target. It writes entry, with the
	// resolution's ID added to its details as resolution_id, in the same
	// transaction. Without a resolution it is domain.ErrCaseNotResolved; a
	// report opened meanwhile that collides is ErrConflict.
	Reopen(ctx context.Context, account uuid.UUID, at time.Time, entry audit.Entry) error
}

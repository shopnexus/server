// Package repository stores module chotot's codes, links and imports in
// Postgres. Each method is its own transaction.
package repository

import (
	"context"
	"time"
	"uuid"

	"github.com/shopnexus/server/internal/chotot/domain"
)

// Codes keeps each account's code.
type Codes interface {
	// Issue returns the account's code live at now, or stores c in place of
	// an expired one and returns it. A deleted account is
	// accountdomain.ErrSessionRevoked.
	Issue(ctx context.Context, account uuid.UUID, c domain.Code, now time.Time) (domain.Code, error)
	// Get returns the account's code, live or not; false for none.
	Get(ctx context.Context, account uuid.UUID) (domain.Code, bool, error)
}

// Links keeps each account's linked profile.
type Links interface {
	// Link stores l as its account's link, in place of the account's link
	// and of any link of l.OID, and uses up the account's code, which must
	// still be code: otherwise domain.ErrCodeExpired. Linking the profile
	// the account links already keeps its settings. A deleted account is
	// accountdomain.ErrSessionRevoked.
	Link(ctx context.Context, l domain.Link, code string) (domain.Link, error)
	// Get returns the account's link; false for none.
	Get(ctx context.Context, account uuid.UUID) (domain.Link, bool, error)
	// ByID returns a link; false for none.
	ByID(ctx context.Context, id uuid.UUID) (domain.Link, bool, error)
	// Update changes the account's settings p names; false for no link.
	Update(ctx context.Context, account uuid.UUID, p domain.SettingsPatch) (domain.Link, bool, error)
	// Checked records a check of link id at: the listings that could still
	// be brought over and the newest listing seen. A gone link is fine.
	Checked(ctx context.Context, id uuid.UUID, at time.Time, waiting int, seenThrough int64) error
	// Imported announces that a check of link id brought count listings
	// over, as event, unless the link is gone.
	Imported(ctx context.Context, id uuid.UUID, count int, event uuid.UUID, at time.Time) error
	// Delete removes the account's link; none is fine.
	Delete(ctx context.Context, account uuid.UUID) error
	// DeleteAccount removes the account's code, link and imports.
	DeleteAccount(ctx context.Context, account uuid.UUID) error
}

// Imports keeps each account's imports of Chợ Tốt listings.
type Imports interface {
	// Start starts each of imps, one account's, unless that account's
	// import of the same listing is settled at now, and returns each
	// listing's import as it then is, in the order of imps. A deleted
	// account is accountdomain.ErrSessionRevoked.
	Start(ctx context.Context, imps []domain.Import, now time.Time) ([]domain.Import, error)
	// Save writes imp while its row is still imp.ID in state from; false
	// when it is not.
	Save(ctx context.Context, imp domain.Import, from domain.ImportState) (bool, error)
	// Get returns the account's import of a listing; false for none.
	Get(ctx context.Context, account uuid.UUID, listID int64) (domain.Import, bool, error)
	// ByID returns an import; false for none.
	ByID(ctx context.Context, id uuid.UUID) (domain.Import, bool, error)
	// ByListing returns the import that posted a listing; false for none.
	ByListing(ctx context.Context, listing uuid.UUID) (domain.Import, bool, error)
	// List returns the account's imports, newest listing first.
	List(ctx context.Context, account uuid.UUID) ([]domain.Import, error)
	// Followed records the price seen on Chợ Tốt of a listed import, and
	// whether its listing left Chợ Tốt.
	Followed(ctx context.Context, id uuid.UUID, adPrice int64, adGone bool) error
	// ClearReview empties the review of the import that posted a listing.
	ClearReview(ctx context.Context, listing uuid.UUID) error
	// DeleteListing removes the import that posted a listing; none is fine.
	DeleteListing(ctx context.Context, listing uuid.UUID) error
	// Sold announces, as event, that the copy an import posted sold.
	Sold(ctx context.Context, id, event uuid.UUID, at time.Time) error
}

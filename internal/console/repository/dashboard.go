package repository

import (
	"context"
	"time"

	"github.com/shopnexus/server2/internal/console/domain"
)

// Dashboards keeps the tickets and sessions of the ops dashboards. Each
// method is one statement.
type Dashboards interface {
	// CreateTicket stores t and deletes the expired tickets of its console
	// session.
	CreateTicket(ctx context.Context, t domain.DashboardTicket) error
	// Redeem marks the ticket whose token hashes to ticketHash used and
	// stores a dashboard session for app, whose cookie's token hashes to
	// tokenHash, on the ticket's console session. It returns the session's
	// end, the console session's, or ErrNotFound unless at now the ticket is
	// unused, unexpired, for app, and its console session lives.
	Redeem(ctx context.Context, ticketHash, tokenHash []byte, app domain.App, now time.Time) (expiresAt time.Time, err error)
	// Live is a query joining the dashboard session whose token hashes to
	// tokenHash with its console session and member. It returns ErrNotFound
	// unless the session is for app and its console session lives at now.
	Live(ctx context.Context, tokenHash []byte, app domain.App, now time.Time) (domain.DashboardGrant, error)
}

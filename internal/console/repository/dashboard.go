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
	// stores s for the ticket's console session, ending with it. It returns s
	// with SessionID and ExpiresAt set, or ErrNotFound unless at
	// s.CreatedAt the ticket is unused, unexpired, for s.App, and its console
	// session lives.
	Redeem(ctx context.Context, ticketHash []byte, s domain.DashboardSession) (domain.DashboardSession, error)
	// Live is a query joining the dashboard session whose token hashes to
	// tokenHash with its console session and member. It returns ErrNotFound
	// unless the session is for app and its console session lives at now.
	Live(ctx context.Context, tokenHash []byte, app domain.App, now time.Time) (domain.DashboardGrant, error)
}

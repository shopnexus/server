package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/console/domain"
)

type pgDashboards struct{ db *pgxpool.Pool }

func NewDashboards(db *pgxpool.Pool) Dashboards { return pgDashboards{db} }

func (st pgDashboards) CreateTicket(ctx context.Context, t domain.DashboardTicket) error {
	_, err := st.db.Exec(ctx, `
WITH expired AS (
    DELETE FROM console_dashboard_tickets
    WHERE session_id = @session_id AND expires_at <= @created_at
)
INSERT INTO console_dashboard_tickets (token_hash, session_id, app, created_at, expires_at)
VALUES (@token_hash, @session_id, @app, @created_at, @expires_at)`, pgx.StrictNamedArgs{
		"token_hash": t.TokenHash, "session_id": t.SessionID, "app": t.App,
		"created_at": t.CreatedAt, "expires_at": t.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("insert dashboard ticket: %w", err)
	}
	return nil
}

// Redeem's UPDATE takes the ticket's row lock, so a second callback with
// the same ticket waits, finds used_at set and matches nothing.
func (st pgDashboards) Redeem(ctx context.Context, ticketHash, tokenHash []byte, app domain.App, now time.Time) (time.Time, error) {
	var expiresAt time.Time
	err := st.db.QueryRow(ctx, `
WITH ticket AS (
    UPDATE console_dashboard_tickets t SET used_at = @now
    FROM console_sessions s
    JOIN console_members m ON m.id = s.member_id
    WHERE t.token_hash = @ticket_hash AND t.app = @app AND t.used_at IS NULL AND t.expires_at > @now
      AND s.id = t.session_id AND `+sessionLives+`
    RETURNING t.session_id, s.expires_at
)
INSERT INTO console_dashboard_sessions (token_hash, session_id, app, created_at, expires_at)
SELECT @token_hash, ticket.session_id, @app, @now, ticket.expires_at FROM ticket
RETURNING expires_at`, pgx.StrictNamedArgs{
		"ticket_hash": ticketHash, "token_hash": tokenHash, "app": app, "now": now,
	}).Scan(&expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("redeem dashboard ticket: %w", err)
	}
	return expiresAt, nil
}

func (st pgDashboards) Live(ctx context.Context, tokenHash []byte, app domain.App, now time.Time) (domain.DashboardGrant, error) {
	var g domain.DashboardGrant
	err := st.db.QueryRow(ctx, `
SELECT m.email, m.console_role
FROM console_dashboard_sessions d
JOIN console_sessions s ON s.id = d.session_id
JOIN console_members m ON m.id = s.member_id
WHERE d.token_hash = @token_hash AND d.app = @app AND `+sessionLives,
		pgx.StrictNamedArgs{"token_hash": tokenHash, "app": app, "now": now}).Scan(&g.Email, &g.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DashboardGrant{}, ErrNotFound
	}
	if err != nil {
		return domain.DashboardGrant{}, fmt.Errorf("select dashboard session: %w", err)
	}
	return g, nil
}

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/staff/domain"
)

type pgDashboards struct{ db *pgxpool.Pool }

func NewDashboards(db *pgxpool.Pool) Dashboards { return pgDashboards{db} }

func (st pgDashboards) CreateTicket(ctx context.Context, t domain.DashboardTicket) error {
	_, err := st.db.Exec(ctx, `
WITH expired AS (
    DELETE FROM staff_dashboard_tickets
    WHERE staff_session_id = @staff_session_id AND expires_at <= @created_at
)
INSERT INTO staff_dashboard_tickets (token_hash, staff_session_id, app, created_at, expires_at)
VALUES (@token_hash, @staff_session_id, @app, @created_at, @expires_at)`, pgx.StrictNamedArgs{
		"token_hash": t.TokenHash, "staff_session_id": t.StaffSessionID, "app": t.App,
		"created_at": t.CreatedAt, "expires_at": t.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("insert dashboard ticket: %w", err)
	}
	return nil
}

// Redeem's UPDATE takes the ticket's row lock, so a second callback with
// the same ticket waits, finds used_at set and matches nothing.
func (st pgDashboards) Redeem(ctx context.Context, ticketHash []byte, s domain.DashboardSession) (domain.DashboardSession, error) {
	err := st.db.QueryRow(ctx, `
WITH ticket AS (
    UPDATE staff_dashboard_tickets t SET used_at = @now
    FROM staff_sessions s
    JOIN staff_members m ON m.id = s.staff_id
    WHERE t.token_hash = @ticket_hash AND t.app = @app AND t.used_at IS NULL AND t.expires_at > @now
      AND s.id = t.staff_session_id AND `+sessionLives+`
    RETURNING t.staff_session_id, s.expires_at
)
INSERT INTO staff_dashboard_sessions (token_hash, staff_session_id, app, created_at, expires_at)
SELECT @token_hash, ticket.staff_session_id, @app, @now, ticket.expires_at FROM ticket
RETURNING staff_session_id, expires_at`, pgx.StrictNamedArgs{
		"ticket_hash": ticketHash, "token_hash": s.TokenHash, "app": s.App, "now": s.CreatedAt,
	}).Scan(&s.StaffSessionID, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DashboardSession{}, ErrNotFound
	}
	if err != nil {
		return domain.DashboardSession{}, fmt.Errorf("redeem dashboard ticket: %w", err)
	}
	return s, nil
}

func (st pgDashboards) Live(ctx context.Context, tokenHash []byte, app domain.App, now time.Time) (domain.DashboardGrant, error) {
	var g domain.DashboardGrant
	err := st.db.QueryRow(ctx, `
SELECT m.email, m.staff_role
FROM staff_dashboard_sessions d
JOIN staff_sessions s ON s.id = d.staff_session_id
JOIN staff_members m ON m.id = s.staff_id
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

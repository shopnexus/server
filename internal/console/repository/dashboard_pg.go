package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/audit"
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

func (st pgDashboards) Floors(ctx context.Context) ([]domain.DashboardFloor, error) {
	rows, err := st.db.Query(ctx, `SELECT app, display_name, floor_role FROM console_dashboards ORDER BY sort_order`)
	if err != nil {
		return nil, fmt.Errorf("select dashboards: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.DashboardFloor, error) {
		var f domain.DashboardFloor
		if scanErr := r.Scan(&f.App, &f.Name, &f.Floor); scanErr != nil {
			return f, fmt.Errorf("scan dashboard: %w", scanErr)
		}
		return f, nil
	})
	if err != nil {
		return nil, fmt.Errorf("read dashboards: %w", err)
	}
	return out, nil
}

func (st pgDashboards) Floor(ctx context.Context, app domain.App) (domain.Role, error) {
	var floor domain.Role
	err := st.db.QueryRow(ctx, `SELECT floor_role FROM console_dashboards WHERE app = @app`,
		pgx.StrictNamedArgs{"app": app}).Scan(&floor)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("select dashboard floor: %w", err)
	}
	return floor, nil
}

func (st pgDashboards) SetFloors(ctx context.Context, floors map[domain.App]domain.Role, by uuid.UUID, at time.Time,
	entries []audit.Entry,
) error {
	apps, roles := make([]string, 0, len(floors)), make([]string, 0, len(floors))
	for app, floor := range floors {
		apps, roles = append(apps, string(app)), append(roles, string(floor))
	}
	b := &pgx.Batch{}
	b.Queue(`UPDATE console_dashboards d SET floor_role = f.floor, updated_by = @by, updated_at = @at
FROM unnest(@apps::text[], @floors::text[]) AS f(app, floor) WHERE d.app = f.app`,
		pgx.StrictNamedArgs{"apps": apps, "floors": roles, "by": by, "at": at}).Fn = func(br pgx.BatchResults) error {
		tag, err := br.Exec()
		if err != nil {
			return fmt.Errorf("update dashboard floors: %w", err)
		}
		if tag.RowsAffected() != int64(len(apps)) {
			return ErrNotFound
		}
		return nil
	}
	audit.Queue(b, entries...)
	err := pgx.BeginFunc(ctx, st.db, func(tx pgx.Tx) error {
		return tx.SendBatch(ctx, b).Close()
	})
	if err != nil {
		return fmt.Errorf("set dashboard floors: %w", err)
	}
	return nil
}

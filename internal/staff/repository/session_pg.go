package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/staff/domain"
)

type pgSessions struct{ db *pgxpool.Pool }

func NewSessions(db *pgxpool.Pool) Sessions { return pgSessions{db} }

func (st pgSessions) Create(ctx context.Context, s domain.Session) error {
	_, err := st.db.Exec(ctx, `
INSERT INTO staff_sessions (id, staff_id, token_hash, credentials_version, ip, user_agent, created_at, expires_at)
VALUES (@id, @staff_id, @token_hash, @credentials_version, @ip, @user_agent, @created_at, @expires_at)`, pgx.StrictNamedArgs{
		"id": s.ID, "staff_id": s.StaffID, "token_hash": s.TokenHash, "credentials_version": s.CredentialsVersion,
		"ip": s.IP, "user_agent": s.UserAgent, "created_at": s.CreatedAt, "expires_at": s.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("insert staff session: %w", err)
	}
	return nil
}

const liveSession = `
SELECT s.id, s.staff_id, m.role
FROM staff_sessions s
JOIN staff_members m ON m.id = s.staff_id
WHERE s.token_hash = @token_hash AND s.revoked_at IS NULL AND s.expires_at > @now
  AND m.status = 'active' AND m.credentials_version = s.credentials_version`

func (st pgSessions) Live(ctx context.Context, tokenHash []byte, now time.Time) (domain.Grant, error) {
	var g domain.Grant
	err := st.db.QueryRow(ctx, liveSession, pgx.StrictNamedArgs{"token_hash": tokenHash, "now": now}).
		Scan(&g.SessionID, &g.StaffID, &g.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Grant{}, ErrNotFound
	}
	if err != nil {
		return domain.Grant{}, fmt.Errorf("select staff session: %w", err)
	}
	return g, nil
}

func (st pgSessions) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	_, err := st.db.Exec(ctx, `UPDATE staff_sessions SET revoked_at = @now WHERE id = @id AND revoked_at IS NULL`,
		pgx.StrictNamedArgs{"id": id, "now": now})
	if err != nil {
		return fmt.Errorf("revoke staff session: %w", err)
	}
	return nil
}

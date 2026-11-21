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
)

type pgSessions struct{ db *pgxpool.Pool }

func NewSessions(db *pgxpool.Pool) Sessions { return pgSessions{db} }

func (st pgSessions) Create(ctx context.Context, s domain.Session) error {
	_, err := st.db.Exec(ctx, `
INSERT INTO console_sessions (id, member_id, token_hash, credentials_version, ip, user_agent, created_at, expires_at)
VALUES (@id, @member_id, @token_hash, @credentials_version, @ip, @user_agent, @created_at, @expires_at)`, pgx.StrictNamedArgs{
		"id": s.ID, "member_id": s.MemberID, "token_hash": s.TokenHash, "credentials_version": s.CredentialsVersion,
		"ip": s.IP, "user_agent": s.UserAgent, "created_at": s.CreatedAt, "expires_at": s.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("insert console session: %w", err)
	}
	return nil
}

// sessionLives holds a console session s, joined with its member m, to the
// rules of a live session at @now: unrevoked, unexpired, and its member
// active at the credentials version it was issued at. Dashboard sessions
// use it too, so they end with their console session.
const sessionLives = `s.revoked_at IS NULL AND s.expires_at > @now
  AND m.status = 'active' AND m.credentials_version = s.credentials_version`

const liveSession = `
SELECT s.id, s.member_id, m.console_role
FROM console_sessions s
JOIN console_members m ON m.id = s.member_id
WHERE s.token_hash = @token_hash AND ` + sessionLives

func (st pgSessions) Live(ctx context.Context, tokenHash []byte, now time.Time) (domain.Grant, error) {
	var g domain.Grant
	err := st.db.QueryRow(ctx, liveSession, pgx.StrictNamedArgs{"token_hash": tokenHash, "now": now}).
		Scan(&g.SessionID, &g.MemberID, &g.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Grant{}, ErrNotFound
	}
	if err != nil {
		return domain.Grant{}, fmt.Errorf("select console session: %w", err)
	}
	return g, nil
}

func (st pgSessions) LastSignIns(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]time.Time, error) {
	rows, err := st.db.Query(ctx, `
SELECT member_id, max(created_at) FROM console_sessions WHERE member_id = ANY(@ids) GROUP BY member_id`,
		pgx.StrictNamedArgs{"ids": ids})
	if err != nil {
		return nil, fmt.Errorf("select console last sign-ins: %w", err)
	}
	out := map[uuid.UUID]time.Time{}
	var id uuid.UUID
	var at time.Time
	_, err = pgx.ForEachRow(rows, []any{&id, &at}, func() error {
		out[id] = at
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan console last sign-ins: %w", err)
	}
	return out, nil
}

func (st pgSessions) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	_, err := st.db.Exec(ctx, `UPDATE console_sessions SET revoked_at = @now WHERE id = @id AND revoked_at IS NULL`,
		pgx.StrictNamedArgs{"id": id, "now": now})
	if err != nil {
		return fmt.Errorf("revoke console session: %w", err)
	}
	return nil
}

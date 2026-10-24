package session

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
)

type pgStore struct{ db *pgxpool.Pool }

func NewPGStore(db *pgxpool.Pool) Store { return pgStore{db} }

const columns = `id, staff_id, refresh_hash, prev_refresh_hash, credentials_version, ip, user_agent,
       created_at, last_used_at, expires_at, revoked_at, write_id`

func scan(row pgx.Row) (Session, error) {
	var (
		s  Session
		ip netip.Prefix
	)
	err := row.Scan(&s.id, &s.staffID, &s.refreshHash, &s.prevRefreshHash, &s.credentialsVersion, &ip, &s.userAgent,
		&s.createdAt, &s.lastUsedAt, &s.expiresAt, &s.revokedAt, &s.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("select staff session: %w", err)
	}
	s.ip = ip.Addr()
	return s, nil
}

const insertSession = `
INSERT INTO staff_sessions (` + columns + `)
VALUES (@id, @staff_id, @refresh_hash, NULL, @credentials_version, @ip, @user_agent,
        @created_at, @created_at, @expires_at, NULL, @write_id)`

func (st pgStore) CreateAudited(ctx context.Context, s Session, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, st.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, insertSession, pgx.StrictNamedArgs{
			"id": s.id, "staff_id": s.staffID, "refresh_hash": s.refreshHash, "credentials_version": s.credentialsVersion,
			"ip": s.ip, "user_agent": s.userAgent, "created_at": s.createdAt, "expires_at": s.expiresAt, "write_id": s.writeID,
		}); err != nil {
			return fmt.Errorf("insert staff session: %w", err)
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("create staff session: %w", err)
	}
	return nil
}

func (st pgStore) Get(ctx context.Context, id uuid.UUID) (Session, error) {
	return scan(st.db.QueryRow(ctx, `SELECT `+columns+` FROM staff_sessions WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (st pgStore) FindByRefresh(ctx context.Context, hash []byte) (Session, error) {
	return scan(st.db.QueryRow(ctx, `SELECT `+columns+` FROM staff_sessions WHERE refresh_hash = @hash`, pgx.StrictNamedArgs{"hash": hash}))
}

func (st pgStore) FindByPrevRefresh(ctx context.Context, hash []byte) (Session, error) {
	return scan(st.db.QueryRow(ctx, `SELECT `+columns+` FROM staff_sessions WHERE prev_refresh_hash = @hash LIMIT 1`,
		pgx.StrictNamedArgs{"hash": hash}))
}

const saveSession = `
UPDATE staff_sessions
SET refresh_hash = @refresh_hash, prev_refresh_hash = @prev_refresh_hash, credentials_version = @credentials_version,
    last_used_at = @last_used_at, revoked_at = @revoked_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func update(ctx context.Context, q execer, s *Session, writeID uuid.UUID) error {
	tag, err := q.Exec(ctx, saveSession, pgx.StrictNamedArgs{
		"id": s.id, "read_write_id": s.writeID, "write_id": writeID, "refresh_hash": s.refreshHash,
		"prev_refresh_hash": s.prevRefreshHash, "credentials_version": s.credentialsVersion,
		"last_used_at": s.lastUsedAt, "revoked_at": s.revokedAt,
	})
	if err != nil {
		return fmt.Errorf("update staff session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (st pgStore) Save(ctx context.Context, s *Session, writeID uuid.UUID) error {
	if err := update(ctx, st.db, s, writeID); err != nil {
		return err
	}
	s.writeID = writeID
	return nil
}

func (st pgStore) SaveAudited(ctx context.Context, s *Session, writeID uuid.UUID, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, st.db, func(tx pgx.Tx) error {
		if err := update(ctx, tx, s, writeID); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("save staff session with audit: %w", err)
	}
	s.writeID = writeID
	return nil
}

const grant = `
SELECT m.role, m.status = 'active', s.credentials_version = m.credentials_version, s.revoked_at, s.expires_at
FROM staff_sessions s
JOIN staff_members m ON m.id = s.staff_id
WHERE s.id = @id AND s.staff_id = @staff_id`

func (st pgStore) Grant(ctx context.Context, id, staffID uuid.UUID) (Grant, error) {
	var g Grant
	err := st.db.QueryRow(ctx, grant, pgx.StrictNamedArgs{"id": id, "staff_id": staffID}).
		Scan(&g.Role, &g.Active, &g.Current, &g.RevokedAt, &g.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	if err != nil {
		return Grant{}, fmt.Errorf("select staff grant: %w", err)
	}
	return g, nil
}

func (st pgStore) MoveVersion(ctx context.Context, id uuid.UUID, from, to int) error {
	tag, err := st.db.Exec(ctx, `
UPDATE staff_sessions SET credentials_version = @to, write_id = @write_id
WHERE id = @id AND credentials_version = @from AND revoked_at IS NULL`,
		pgx.StrictNamedArgs{"id": id, "from": from, "to": to, "write_id": uuid.NewV7()})
	if err != nil {
		return fmt.Errorf("move staff session version: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRevoked
	}
	return nil
}

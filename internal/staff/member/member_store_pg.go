package member

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/page"
)

// pgStore seals TOTP secrets with AES-GCM, binding each ciphertext to its
// member's id, so a secret copied to another row does not open.
type pgStore struct {
	db   *pgxpool.Pool
	seal cipher.AEAD
}

func NewPGStore(db *pgxpool.Pool, seal cipher.AEAD) Store { return pgStore{db: db, seal: seal} }

const columns = `id, email, display_name, role, status, password_hash, must_change_password, credentials_version,
       totp_secret, totp_pending_secret, totp_last_step, created_at, updated_at, write_id`

func (s pgStore) sealed(id uuid.UUID, plain []byte) []byte {
	if plain == nil {
		return nil
	}
	nonce := make([]byte, s.seal.NonceSize())
	rand.Read(nonce)
	return s.seal.Seal(nonce, nonce, plain, id[:])
}

func (s pgStore) open(id uuid.UUID, sealed []byte) ([]byte, error) {
	if sealed == nil {
		return nil, nil
	}
	n := s.seal.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("sealed secret too short")
	}
	plain, err := s.seal.Open(nil, sealed[:n], sealed[n:], id[:])
	if err != nil {
		return nil, fmt.Errorf("open totp secret: %w", err)
	}
	return plain, nil
}

func (s pgStore) scan(row pgx.Row) (Member, error) {
	var (
		m               Member
		secret, pending []byte
	)
	err := row.Scan(&m.id, &m.email, &m.displayName, &m.role, &m.status, &m.passwordHash, &m.mustChangePassword,
		&m.credentialsVersion, &secret, &pending, &m.totpLastStep, &m.createdAt, &m.updatedAt, &m.writeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("select staff member: %w", err)
	}
	if m.totpSecret, err = s.open(m.id, secret); err != nil {
		return Member{}, err
	}
	if m.totpPending, err = s.open(m.id, pending); err != nil {
		return Member{}, err
	}
	return m, nil
}

const insertMember = `
INSERT INTO staff_members (` + columns + `)
VALUES (@id, @email, @display_name, @role, @status, @password_hash, @must_change_password, @credentials_version,
        @totp_secret, @totp_pending_secret, @totp_last_step, @created_at, @updated_at, @write_id)
ON CONFLICT (id) DO NOTHING`

func (s pgStore) CreateAudited(ctx context.Context, m Member, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, insertMember, pgx.StrictNamedArgs{
			"id": m.id, "email": m.email, "display_name": m.displayName, "role": m.role, "status": m.status,
			"password_hash": m.passwordHash, "must_change_password": m.mustChangePassword,
			"credentials_version": m.credentialsVersion, "totp_secret": s.sealed(m.id, m.totpSecret),
			"totp_pending_secret": s.sealed(m.id, m.totpPending), "totp_last_step": m.totpLastStep,
			"created_at": m.createdAt, "updated_at": m.updatedAt, "write_id": m.writeID,
		})
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.ConstraintName == "staff_members_email" {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("insert staff member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil // the same id again
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("create staff member: %w", err)
	}
	return nil
}

func (s pgStore) Get(ctx context.Context, id uuid.UUID) (Member, error) {
	return s.scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM staff_members WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (s pgStore) FindByEmail(ctx context.Context, email string) (Member, error) {
	return s.scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM staff_members WHERE email = @email`, pgx.StrictNamedArgs{"email": email}))
}

func (s pgStore) List(ctx context.Context, p page.Request) ([]Member, error) {
	rows, err := s.db.Query(ctx, `SELECT `+columns+` FROM staff_members WHERE id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select staff members: %w", err)
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		m, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read staff members: %w", err)
	}
	return out, nil
}

const saveMember = `
UPDATE staff_members
SET display_name = @display_name, role = @role, status = @status, password_hash = @password_hash,
    must_change_password = @must_change_password, credentials_version = @credentials_version,
    totp_secret = @totp_secret, totp_pending_secret = @totp_pending_secret, totp_last_step = @totp_last_step,
    updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (s pgStore) update(ctx context.Context, q execer, m *Member, writeID uuid.UUID) error {
	tag, err := q.Exec(ctx, saveMember, pgx.StrictNamedArgs{
		"id": m.id, "read_write_id": m.writeID, "write_id": writeID, "display_name": m.displayName, "role": m.role,
		"status": m.status, "password_hash": m.passwordHash, "must_change_password": m.mustChangePassword,
		"credentials_version": m.credentialsVersion, "totp_secret": s.sealed(m.id, m.totpSecret),
		"totp_pending_secret": s.sealed(m.id, m.totpPending), "totp_last_step": m.totpLastStep, "updated_at": m.updatedAt,
	})
	if err != nil {
		return fmt.Errorf("update staff member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s pgStore) Save(ctx context.Context, m *Member, writeID uuid.UUID) error {
	if err := s.update(ctx, s.db, m, writeID); err != nil {
		return err
	}
	m.writeID = writeID
	return nil
}

func (s pgStore) SaveAudited(ctx context.Context, m *Member, writeID uuid.UUID, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.update(ctx, tx, m, writeID); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("save staff member with audit: %w", err)
	}
	m.writeID = writeID
	return nil
}

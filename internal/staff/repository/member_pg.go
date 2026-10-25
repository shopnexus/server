package repository

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
	"github.com/shopnexus/server2/internal/staff/domain"
)

// pgMembers seals TOTP secrets with AES-GCM, binding each ciphertext to its
// member's id, so a secret copied to another row does not open.
type pgMembers struct {
	db   *pgxpool.Pool
	seal cipher.AEAD
}

func NewMembers(db *pgxpool.Pool, seal cipher.AEAD) Members { return pgMembers{db: db, seal: seal} }

const columns = `id, email, display_name, role, status, password_hash, must_change_password, credentials_version,
       totp_secret, totp_pending_secret, totp_last_step, created_at, updated_at, write_id`

func (s pgMembers) sealed(id uuid.UUID, plain []byte) []byte {
	if plain == nil {
		return nil
	}
	nonce := make([]byte, s.seal.NonceSize())
	rand.Read(nonce)
	return s.seal.Seal(nonce, nonce, plain, id[:])
}

func (s pgMembers) open(id uuid.UUID, sealed []byte) ([]byte, error) {
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

func (s pgMembers) scan(row pgx.Row) (domain.Member, error) {
	var (
		st              domain.MemberState
		secret, pending []byte
	)
	err := row.Scan(&st.ID, &st.Email, &st.DisplayName, &st.Role, &st.Status, &st.PasswordHash, &st.MustChangePassword,
		&st.CredentialsVersion, &secret, &pending, &st.TOTPLastStep, &st.CreatedAt, &st.UpdatedAt, &st.WriteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, ErrNotFound
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("select staff member: %w", err)
	}
	if st.TOTPSecret, err = s.open(st.ID, secret); err != nil {
		return domain.Member{}, err
	}
	if st.TOTPPending, err = s.open(st.ID, pending); err != nil {
		return domain.Member{}, err
	}
	return domain.RestoreMember(st), nil
}

const insertMember = `
INSERT INTO staff_members (` + columns + `)
VALUES (@id, @email, @display_name, @role, @status, @password_hash, @must_change_password, @credentials_version,
        @totp_secret, @totp_pending_secret, @totp_last_step, @created_at, @updated_at, @write_id)
ON CONFLICT (id) DO NOTHING`

func (s pgMembers) CreateAudited(ctx context.Context, m domain.Member, e audit.Entry) error {
	st := m.State()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, insertMember, pgx.StrictNamedArgs{
			"id": st.ID, "email": st.Email, "display_name": st.DisplayName, "role": st.Role, "status": st.Status,
			"password_hash": st.PasswordHash, "must_change_password": st.MustChangePassword,
			"credentials_version": st.CredentialsVersion, "totp_secret": s.sealed(st.ID, st.TOTPSecret),
			"totp_pending_secret": s.sealed(st.ID, st.TOTPPending), "totp_last_step": st.TOTPLastStep,
			"created_at": st.CreatedAt, "updated_at": st.UpdatedAt, "write_id": st.WriteID,
		})
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.ConstraintName == "staff_members_email" {
			return domain.ErrEmailTaken
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

func (s pgMembers) Get(ctx context.Context, id uuid.UUID) (domain.Member, error) {
	return s.scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM staff_members WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (s pgMembers) FindByEmail(ctx context.Context, email string) (domain.Member, error) {
	return s.scan(s.db.QueryRow(ctx, `SELECT `+columns+` FROM staff_members WHERE email = @email`, pgx.StrictNamedArgs{"email": email}))
}

func (s pgMembers) List(ctx context.Context, p page.Request) ([]domain.Member, error) {
	rows, err := s.db.Query(ctx, `SELECT `+columns+` FROM staff_members WHERE id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select staff members: %w", err)
	}
	defer rows.Close()
	var out []domain.Member
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

func (s pgMembers) update(ctx context.Context, q execer, m *domain.Member, writeID uuid.UUID) error {
	st := m.State()
	tag, err := q.Exec(ctx, saveMember, pgx.StrictNamedArgs{
		"id": st.ID, "read_write_id": st.WriteID, "write_id": writeID, "display_name": st.DisplayName, "role": st.Role,
		"status": st.Status, "password_hash": st.PasswordHash, "must_change_password": st.MustChangePassword,
		"credentials_version": st.CredentialsVersion, "totp_secret": s.sealed(st.ID, st.TOTPSecret),
		"totp_pending_secret": s.sealed(st.ID, st.TOTPPending), "totp_last_step": st.TOTPLastStep, "updated_at": st.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("update staff member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s pgMembers) Save(ctx context.Context, m *domain.Member, writeID uuid.UUID) error {
	if err := s.update(ctx, s.db, m, writeID); err != nil {
		return err
	}
	m.Saved(writeID)
	return nil
}

func (s pgMembers) SaveAudited(ctx context.Context, m *domain.Member, writeID uuid.UUID, e audit.Entry) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.update(ctx, tx, m, writeID); err != nil {
			return err
		}
		return audit.Insert(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("save staff member with audit: %w", err)
	}
	m.Saved(writeID)
	return nil
}

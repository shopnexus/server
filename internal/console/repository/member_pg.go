package repository

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server/internal/console/domain"
	"github.com/shopnexus/server/internal/platform/audit"
	"github.com/shopnexus/server/internal/platform/page"
	"github.com/shopnexus/server/internal/platform/secret"
)

// pgMembers seals TOTP secrets with AES-GCM, binding each ciphertext to its
// member's id, so a secret copied to another row does not open.
type pgMembers struct {
	db   *pgxpool.Pool
	seal *secret.Box
}

func NewMembers(db *pgxpool.Pool, seal *secret.Box) Members { return pgMembers{db: db, seal: seal} }

const columns = `id, email, display_name, console_role, status, password_hash, must_change_password, credentials_version,
       totp_secret, totp_pending_secret, totp_last_step, created_at, updated_at, write_id`

func (s pgMembers) sealed(id uuid.UUID, plain []byte) []byte {
	if plain == nil {
		return nil
	}
	return s.seal.Seal(plain, id[:])
}

func (s pgMembers) open(id uuid.UUID, sealed []byte) ([]byte, error) {
	if sealed == nil {
		return nil, nil
	}
	plain, err := s.seal.Open(sealed, id[:])
	if err != nil {
		return nil, fmt.Errorf("open totp secret: %w", err)
	}
	return plain, nil
}

// scan reads columns and opens the TOTP secrets.
func (s pgMembers) scan(row pgx.CollectableRow) (domain.Member, error) {
	var (
		st            domain.MemberState
		totp, pending []byte
	)
	err := row.Scan(&st.ID, &st.Email, &st.DisplayName, &st.Role, &st.Status, &st.PasswordHash, &st.MustChangePassword,
		&st.CredentialsVersion, &totp, &pending, &st.TOTPLastStep, &st.CreatedAt, &st.UpdatedAt, &st.WriteID)
	if err != nil {
		return domain.Member{}, fmt.Errorf("scan console member: %w", err)
	}
	if st.TOTPSecret, err = s.open(st.ID, totp); err != nil {
		return domain.Member{}, err
	}
	if st.TOTPPending, err = s.open(st.ID, pending); err != nil {
		return domain.Member{}, err
	}
	return domain.RestoreMember(st), nil
}

const insertMember = `
INSERT INTO console_members (` + columns + `)
VALUES (@id, @email, @display_name, @console_role, @status, @password_hash, @must_change_password, @credentials_version,
        @totp_secret, @totp_pending_secret, @totp_last_step, @created_at, @updated_at, @write_id)
ON CONFLICT (id) DO NOTHING`

func (s pgMembers) Create(ctx context.Context, m domain.Member, entry audit.Entry) error {
	st := m.State()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, insertMember, pgx.StrictNamedArgs{
			"id": st.ID, "email": st.Email, "display_name": st.DisplayName, "console_role": st.Role, "status": st.Status,
			"password_hash": st.PasswordHash, "must_change_password": st.MustChangePassword,
			"credentials_version": st.CredentialsVersion, "totp_secret": s.sealed(st.ID, st.TOTPSecret),
			"totp_pending_secret": s.sealed(st.ID, st.TOTPPending), "totp_last_step": st.TOTPLastStep,
			"created_at": st.CreatedAt, "updated_at": st.UpdatedAt, "write_id": st.WriteID,
		})
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.ConstraintName == "console_members_email" {
			return domain.ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("insert console member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil // the same id again
		}
		return audit.Insert(ctx, tx, entry)
	})
	if err != nil {
		return fmt.Errorf("create console member: %w", err)
	}
	return nil
}

func (s pgMembers) Get(ctx context.Context, id uuid.UUID) (domain.Member, error) {
	return s.one(s.db.Query(ctx, `SELECT `+columns+` FROM console_members WHERE id = @id`, pgx.StrictNamedArgs{"id": id}))
}

func (s pgMembers) FindByEmail(ctx context.Context, email string) (domain.Member, error) {
	return s.one(s.db.Query(ctx, `SELECT `+columns+` FROM console_members WHERE email = @email`, pgx.StrictNamedArgs{"email": email}))
}

// one reads the member of a query that selects one at most, or ErrNotFound.
func (s pgMembers) one(rows pgx.Rows, err error) (domain.Member, error) {
	if err != nil {
		return domain.Member{}, fmt.Errorf("select console member: %w", err)
	}
	m, err := pgx.CollectExactlyOneRow(rows, s.scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, ErrNotFound
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("select console member: %w", err)
	}
	return m, nil
}

func (s pgMembers) List(ctx context.Context, p page.Request) ([]domain.Member, error) {
	rows, err := s.db.Query(ctx, `SELECT `+columns+` FROM console_members WHERE id < @before ORDER BY id DESC LIMIT @limit`,
		pgx.StrictNamedArgs{"before": p.Before(), "limit": p.Fetch()})
	if err != nil {
		return nil, fmt.Errorf("select console members: %w", err)
	}
	members, err := pgx.CollectRows(rows, s.scan)
	if err != nil {
		return nil, fmt.Errorf("read console members: %w", err)
	}
	return members, nil
}

const saveMember = `
UPDATE console_members
SET display_name = @display_name, console_role = @console_role, status = @status, password_hash = @password_hash,
    must_change_password = @must_change_password, credentials_version = @credentials_version,
    totp_secret = @totp_secret, totp_pending_secret = @totp_pending_secret, totp_last_step = @totp_last_step,
    updated_at = @updated_at, write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)`

func (s pgMembers) Save(ctx context.Context, m *domain.Member, writeID uuid.UUID, entry *audit.Entry) error {
	st := m.State()
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, saveMember, pgx.StrictNamedArgs{
			"id": st.ID, "read_write_id": st.WriteID, "write_id": writeID, "display_name": st.DisplayName, "console_role": st.Role,
			"status": st.Status, "password_hash": st.PasswordHash, "must_change_password": st.MustChangePassword,
			"credentials_version": st.CredentialsVersion, "totp_secret": s.sealed(st.ID, st.TOTPSecret),
			"totp_pending_secret": s.sealed(st.ID, st.TOTPPending), "totp_last_step": st.TOTPLastStep, "updated_at": st.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("update console member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		if entry == nil {
			return nil
		}
		return audit.Insert(ctx, tx, *entry)
	})
	if err != nil {
		return fmt.Errorf("save console member: %w", err)
	}
	m.Saved(writeID)
	return nil
}

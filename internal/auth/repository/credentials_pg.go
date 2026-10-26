package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shopnexus/server2/internal/auth/domain"
)

type pgCredentials struct{ db *pgxpool.Pool }

func NewCredentials(db *pgxpool.Pool) Credentials { return pgCredentials{db} }

// upsertIdentity inserts an identity or refreshes its last_used_at. The WHERE
// keeps another account's identity untouched, and then no row comes back.
const upsertIdentity = `
INSERT INTO identities (id, account_id, kind, identifier, last_used_at, created_at)
VALUES (@id, @account_id, @kind, @identifier, @last_used_at, @created_at)
ON CONFLICT (kind, identifier) DO UPDATE SET last_used_at = EXCLUDED.last_used_at
WHERE identities.account_id = EXCLUDED.account_id
RETURNING account_id`

func upsertIdentities(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, ids []domain.Identity) error {
	for _, i := range ids {
		var owner uuid.UUID
		err := tx.QueryRow(ctx, upsertIdentity, pgx.StrictNamedArgs{
			"id": i.ID(), "account_id": accountID, "kind": i.Kind(), "identifier": i.Value(),
			"last_used_at": i.LastUsedAt(), "created_at": i.CreatedAt(),
		}).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrTaken
		}
		if err != nil {
			return fmt.Errorf("upsert identity: %w", err)
		}
	}
	return nil
}

func (s pgCredentials) Create(ctx context.Context, c domain.Credentials) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
INSERT INTO credentials (account_id, write_id, updated_at) VALUES (@account_id, @write_id, @updated_at)
ON CONFLICT (account_id) DO NOTHING`,
			pgx.StrictNamedArgs{"account_id": c.AccountID(), "write_id": c.WriteID(), "updated_at": c.UpdatedAt()}); err != nil {
			return fmt.Errorf("insert credentials: %w", err)
		}
		return upsertIdentities(ctx, tx, c.AccountID(), c.Identities())
	})
	if err != nil {
		return fmt.Errorf("create credentials: %w", err)
	}
	return nil
}

func (s pgCredentials) Get(ctx context.Context, accountID uuid.UUID) (domain.Credentials, error) {
	var (
		writeID   uuid.UUID
		updatedAt time.Time
	)
	err := s.db.QueryRow(ctx, `SELECT write_id, updated_at FROM credentials WHERE account_id = @account_id`,
		pgx.StrictNamedArgs{"account_id": accountID}).
		Scan(&writeID, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credentials{}, ErrNotFound
	}
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("select credentials: %w", err)
	}
	rows, err := s.db.Query(ctx, `
SELECT id, kind, identifier, last_used_at, created_at FROM identities WHERE account_id = @account_id ORDER BY created_at, id`,
		pgx.StrictNamedArgs{"account_id": accountID})
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("select identities: %w", err)
	}
	defer rows.Close()
	var identities []domain.Identity
	for rows.Next() {
		var (
			id        uuid.UUID
			kind      domain.Kind
			value     string
			lastUsed  *time.Time
			createdAt time.Time
		)
		if err := rows.Scan(&id, &kind, &value, &lastUsed, &createdAt); err != nil {
			return domain.Credentials{}, fmt.Errorf("scan identity: %w", err)
		}
		identities = append(identities, domain.RestoreIdentity(id, kind, value, lastUsed, createdAt))
	}
	if err := rows.Err(); err != nil {
		return domain.Credentials{}, fmt.Errorf("read identities: %w", err)
	}
	return domain.RestoreCredentials(accountID, identities, writeID, updatedAt), nil
}

func (s pgCredentials) FindIdentity(ctx context.Context, kind domain.Kind, value string) (domain.IdentityRef, error) {
	var ref domain.IdentityRef
	err := s.db.QueryRow(ctx, `SELECT account_id, id FROM identities WHERE kind = @kind AND identifier = @identifier`,
		pgx.StrictNamedArgs{"kind": kind, "identifier": value}).
		Scan(&ref.AccountID, &ref.IdentityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IdentityRef{}, ErrNotFound
	}
	if err != nil {
		return domain.IdentityRef{}, fmt.Errorf("select identity: %w", err)
	}
	return ref, nil
}

func (s pgCredentials) Save(ctx context.Context, c *domain.Credentials, writeID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
UPDATE credentials SET write_id = @write_id, updated_at = @updated_at
WHERE account_id = @account_id AND (write_id = @read_write_id OR write_id = @write_id)`, pgx.StrictNamedArgs{
			"account_id": c.AccountID(), "read_write_id": c.WriteID(), "write_id": writeID, "updated_at": c.UpdatedAt(),
		})
		if err != nil {
			return fmt.Errorf("update credentials: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		identities := c.Identities()
		keep := make([]uuid.UUID, len(identities))
		for n, i := range identities {
			keep[n] = i.ID()
		}
		if _, err := tx.Exec(ctx, `DELETE FROM identities WHERE account_id = @account_id AND NOT (id = ANY(@keep))`,
			pgx.StrictNamedArgs{"account_id": c.AccountID(), "keep": keep}); err != nil {
			return fmt.Errorf("delete removed identities: %w", err)
		}
		return upsertIdentities(ctx, tx, c.AccountID(), identities)
	})
	if err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	c.Saved(writeID)
	return nil
}

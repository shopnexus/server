package credentials

import (
	"context"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct{ db *pgxpool.Pool }

func NewPGStore(db *pgxpool.Pool) Store { return pgStore{db} }

// upsertIdentity inserts an identity or refreshes its last_used_at. The WHERE
// keeps another account's identity untouched, and then no row comes back.
const upsertIdentity = `
INSERT INTO identities (id, account_id, kind, value, last_used_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (kind, value) DO UPDATE SET last_used_at = EXCLUDED.last_used_at
WHERE identities.account_id = EXCLUDED.account_id
RETURNING account_id`

func upsertIdentities(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, ids []Identity) error {
	for _, i := range ids {
		var owner uuid.UUID
		err := tx.QueryRow(ctx, upsertIdentity, i.id, accountID, i.kind, i.value, i.lastUsedAt, i.createdAt).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTaken
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s pgStore) Create(ctx context.Context, c Credentials) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
INSERT INTO credentials (account_id, write_id, updated_at) VALUES ($1, $2, $3)
ON CONFLICT (account_id) DO NOTHING`, c.accountID, c.writeID, c.updatedAt); err != nil {
		return err
	}
	if err := upsertIdentities(ctx, tx, c.accountID, c.identities); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s pgStore) Get(ctx context.Context, accountID uuid.UUID) (Credentials, error) {
	c := Credentials{accountID: accountID}
	err := s.db.QueryRow(ctx, `SELECT write_id, updated_at FROM credentials WHERE account_id = $1`, accountID).
		Scan(&c.writeID, &c.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrNotFound
	}
	if err != nil {
		return Credentials{}, err
	}
	rows, err := s.db.Query(ctx, `
SELECT id, kind, value, last_used_at, created_at FROM identities WHERE account_id = $1 ORDER BY created_at, id`, accountID)
	if err != nil {
		return Credentials{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.id, &i.kind, &i.value, &i.lastUsedAt, &i.createdAt); err != nil {
			return Credentials{}, err
		}
		c.identities = append(c.identities, i)
	}
	return c, rows.Err()
}

func (s pgStore) FindAccount(ctx context.Context, kind Kind, value string) (uuid.UUID, uuid.UUID, error) {
	var accountID, identityID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT account_id, id FROM identities WHERE kind = $1 AND value = $2`, kind, value).
		Scan(&accountID, &identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, uuid.UUID{}, ErrNotFound
	}
	return accountID, identityID, err
}

func (s pgStore) Save(ctx context.Context, c *Credentials, writeID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE credentials SET write_id = $3, updated_at = $4
WHERE account_id = $1 AND (write_id = $2 OR write_id = $3)`, c.accountID, c.writeID, writeID, c.updatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	keep := make([]uuid.UUID, len(c.identities))
	for n, i := range c.identities {
		keep[n] = i.id
	}
	if _, err := tx.Exec(ctx, `DELETE FROM identities WHERE account_id = $1 AND NOT (id = ANY($2))`, c.accountID, keep); err != nil {
		return err
	}
	if err := upsertIdentities(ctx, tx, c.accountID, c.identities); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	c.writeID = writeID
	return nil
}
